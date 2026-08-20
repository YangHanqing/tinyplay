package server

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/filesource"
	"tvremote/internal/player"
	"tvremote/internal/provider"
)

// Host-owned next-episode autoplay. The phone UI may display the countdown
// and issue play-now / cancel, but must never own the timer or next selection:
// a backgrounded or closed browser must still advance playback.

// autoplayGrace is the default countdown; the user can pick 0/5/10s
// (config.AutoplayCountdownPresetSecs) and autoplayCountdown reads that choice.
const autoplayGrace = 5 * time.Second

// loopRunawayShortPlay / loopRunawayLimit stop a loop that has degenerated
// into a relaunch storm: a folder of unplayable files would otherwise restart
// mpv forever, because looping removes the natural "end of list" stop. Five
// consecutive autoplay-started plays that each ended within a few seconds is
// not viewing, it is a fault.
const (
	loopRunawayShortPlay = 10 * time.Second
	loopRunawayLimit     = 5
)

// maxAutoplayEpisodes bounds one whole-series episode fetch. List-loop spans
// the entire show, not one season, so the old 200 would silently truncate long
// runs (One Piece is ~1100) and lose the current episode from the page.
const maxAutoplayEpisodes = 2000

func autoplayCountdown() time.Duration {
	cfg := config.Load()
	return time.Duration(config.NormalizeAutoplayCountdownSecs(cfg.AutoplayCountdownSecs)) * time.Second
}

// autoplayLoopEnabled reports whether the chain wraps instead of stopping.
// Loop is a sub-option of autoplay: with autoplay off nothing chains at all.
func autoplayLoopEnabled() bool {
	cfg := config.Load()
	return cfg.AutoplayNextEpisode && cfg.AutoplayLoopList
}

// loopSingleTitle reports whether a media-server item that has no list to
// chain through (a movie: no series id) should repeat inside mpv instead.
func loopSingleTitle(seriesID string) bool {
	return seriesID == "" && autoplayLoopEnabled()
}

// autoplay status values exposed on GET /api/player/state. There is no
// "finished" status: when no transition will run, the completed context is
// cleared instead so the state does not advertise the finished episode forever.
const (
	autoplayStatusFindingNext   = "finding_next"
	autoplayStatusNextAvailable = "next_available"
)

// autoplayNext is the resolved transition target. Media-server episodes fill
// SeriesID/SeasonID/EpisodeLabel/PosterItemID; file-source next items set
// IsFile + Path (ItemID mirrors Path) and leave the media-server fields empty.
type autoplayNext struct {
	ServerID     string
	ItemID       string
	SeriesID     string
	SeasonID     string
	Title        string
	SeriesTitle  string
	EpisodeLabel string
	PosterItemID string
	// File-source branch (folder-based next-by-filename).
	IsFile bool
	Path   string
	// IsAudio marks the next item as a track rather than a video, which is
	// what lets an album chain without a countdown. See autoplayGraceFor.
	IsAudio bool
	// Restarted marks a transition that wrapped from the end of the list back
	// to its start. Only the phone's wording depends on it; the transition
	// itself is an ordinary one.
	Restarted bool
}

type autoplayState struct {
	generation uint64
	status     string
	deadline   time.Time
	finished   player.PlayContext
	next       *autoplayNext
	// silent suppresses every autoplay field on GET /api/player/state. A
	// zero-grace transition has no countdown to render, and publishing
	// next_available even for one poll would flash the card this exists to
	// avoid — the phone falls back to a full 5s bar when no remaining time
	// accompanies the status.
	silent bool
}

func (s *Server) wireAutoplay() {
	s.player.NaturalEOFReporter = s.onNaturalEOF
}

func (s *Server) now() time.Time {
	if s.autoplayNow != nil {
		return s.autoplayNow()
	}
	return time.Now()
}

func (s *Server) scheduleAfter(d time.Duration, gen uint64, fn func()) {
	s.autoplayMu.Lock()
	defer s.autoplayMu.Unlock()
	if gen != s.autoplay.generation || s.autoplay.status != autoplayStatusNextAvailable {
		return
	}
	if s.autoplayCancel != nil {
		s.autoplayCancel()
		s.autoplayCancel = nil
	}
	if s.autoplayAfter != nil {
		s.autoplayCancel = s.autoplayAfter(d, fn)
		return
	}
	timer := time.AfterFunc(d, fn)
	s.autoplayCancel = func() { timer.Stop() }
}

// cancelAutoplayLocked drops any pending lookup/timer. Caller holds autoplayMu.
func (s *Server) cancelAutoplayLocked() {
	if s.autoplayCancel != nil {
		s.autoplayCancel()
		s.autoplayCancel = nil
	}
	s.autoplay.generation++
	s.autoplay.status = ""
	s.autoplay.deadline = time.Time{}
	s.autoplay.finished = player.PlayContext{}
	s.autoplay.next = nil
	s.autoplay.silent = false
}

// CancelAutoplay invalidates a pending next-episode transition. clearCompleted
// also forgets the finished series context on the player so the UI stops
// advertising a completed episode.
// autoplayBlockThrough returns the highest playback revision whose EOF callback
// an explicit cancel must ignore.
//
// The player increments the revision when terminal cleanup completes, so an EOF
// racing the cancel reports revision+1; that has to be blocked too. But this is
// only possible while no process is running, which is the sole window in which
// such a cleanup can be in flight. With a title actively playing, revision+1 is
// instead that title's own future completion — nothing bumps the revision again
// before its EOF lands — so blocking it would silently kill autoplay for the
// episode currently on screen.
func autoplayBlockThrough(revision uint64, playing bool) uint64 {
	if playing || revision == ^uint64(0) {
		return revision
	}
	return revision + 1
}

func (s *Server) CancelAutoplay(clearCompleted bool) {
	state := s.player.State()
	playing, _ := state["running"].(bool)
	blocked := autoplayBlockThrough(playerStateRevision(state), playing)
	s.autoplayMu.Lock()
	if blocked > s.autoplayCancelledThrough {
		s.autoplayCancelledThrough = blocked
	}
	s.cancelAutoplayLocked()
	// An explicit stop/cancel/manual play ends the automatic chain, so the
	// runaway counter starts clean for whatever the user does next.
	s.autoplayTransitionAt = time.Time{}
	s.autoplayShortRuns = 0
	s.autoplayMu.Unlock()
	if clearCompleted {
		s.player.ClearCompletedPlayback()
	}
}

// noteAutoplayTransition timestamps an automatic transition so the next
// natural EOF can tell viewing from a relaunch storm.
func (s *Server) noteAutoplayTransition() {
	s.autoplayMu.Lock()
	s.autoplayTransitionAt = s.now()
	s.autoplayMu.Unlock()
}

// loopRunawayTripped counts consecutive autoplay-started plays that ended
// almost immediately, and reports when the chain has to be abandoned. Only a
// wrapping list can spin forever — a linear one runs out — so only loop mode
// trips, but the counter itself runs either way and a single normal-length
// play clears it.
func (s *Server) loopRunawayTripped() bool {
	s.autoplayMu.Lock()
	defer s.autoplayMu.Unlock()
	started := s.autoplayTransitionAt
	if started.IsZero() || s.now().Sub(started) >= loopRunawayShortPlay {
		s.autoplayShortRuns = 0
		return false
	}
	s.autoplayShortRuns++
	if !autoplayLoopEnabled() || s.autoplayShortRuns < loopRunawayLimit {
		return false
	}
	s.autoplayShortRuns = 0
	return true
}

// clearCompletedContext forgets the player's post-EOF completed context. The
// player keeps that context solely so host autoplay can coordinate the next
// episode; every path that decides no transition will run must drop it, or
// /api/player/state keeps advertising the finished episode and the phone
// remote repaints it as a permanently "starting" card.
func (s *Server) clearCompletedContext() {
	if s.clearCompleted != nil {
		s.clearCompleted()
		return
	}
	s.player.ClearCompletedPlayback()
}

func (s *Server) onNaturalEOF(ctx player.PlayContext, revision uint64) {
	if !autoplayEligible(ctx) {
		s.clearCompletedContext()
		return
	}
	if s.loopRunawayTripped() {
		log.Printf("autoplay: %d consecutive automatic plays ended within %v; stopping the loop",
			loopRunawayLimit, loopRunawayShortPlay)
		s.CancelAutoplay(true)
		return
	}

	s.autoplayMu.Lock()
	if revision <= s.autoplayCancelledThrough {
		s.autoplayMu.Unlock()
		s.clearCompletedContext()
		return
	}
	// Invalidate any prior pending transition, then claim a fresh generation
	// for this completion (cancelAutoplayLocked already bumps generation).
	s.cancelAutoplayLocked()
	gen := s.autoplay.generation
	s.autoplay.status = autoplayStatusFindingNext
	s.autoplay.finished = ctx
	s.autoplay.next = nil
	s.autoplay.deadline = time.Time{}
	s.autoplayMu.Unlock()

	go s.resolveAndArmAutoplay(gen, ctx)
}

// autoplayEligible reports whether a completed playback can be continued by
// host autoplay at all. Two branches share one setting:
//   - media server (emby/jellyfin/plex): requires SeriesID (same-season scan)
//   - file source (webdav/smb/local/nfs): requires ItemID as the relative path
//
// IPTV / IPTV-catchup / DLNA / live never autoplay.
func autoplayEligible(ctx player.PlayContext) bool {
	if !ctx.PlaybackCompleted || ctx.ItemID == "" {
		return false
	}
	if ctx.IsLive || ctx.SourceType == "iptv" || ctx.SourceType == "iptv-catchup" ||
		ctx.SourceType == "dlna" {
		return false
	}
	if !config.Load().AutoplayNextEpisode {
		return false
	}
	srv := config.GetServer(ctx.ServerID)
	if srv == nil {
		return false
	}
	kind := config.NormalizeServerType(srv.Type)
	if config.IsFileServerType(kind) || config.IsFileServerType(ctx.SourceType) {
		// ItemID holds the relative path written by the file play handler.
		return true
	}
	if ctx.SeriesID == "" {
		return false
	}
	return kind == "emby" || kind == "jellyfin" || kind == "plex"
}

func (s *Server) resolveAndArmAutoplay(gen uint64, finished player.PlayContext) {
	next, err := s.lookupNextEpisode(finished)

	s.autoplayMu.Lock()
	if gen != s.autoplay.generation || s.autoplay.status != autoplayStatusFindingNext {
		s.autoplayMu.Unlock()
		return
	}
	if err != nil || next == nil {
		if err != nil {
			log.Printf("autoplay: next episode lookup failed: %v", err)
		}
		// Season over (or lookup failed): no transition will run, so drop both
		// the pending autoplay state and the player's completed context rather
		// than advertising the finished episode forever.
		s.cancelAutoplayLocked()
		s.autoplayMu.Unlock()
		s.clearCompletedContext()
		return
	}
	grace := autoplayGraceFor(finished, next, autoplayCountdown())
	s.autoplay.next = next
	s.autoplay.status = autoplayStatusNextAvailable
	s.autoplay.silent = grace <= 0
	if grace > 0 {
		s.autoplay.deadline = s.now().Add(grace)
	}
	s.autoplayMu.Unlock()

	s.scheduleAfter(grace, gen, func() {
		s.fireAutoplay(gen)
	})
}

// autoplayGraceFor decides how long the countdown runs before the transition.
//
// A song followed by another song gets none. Skipping straight to the next
// track is what a music player does, and a five-second "up next" card between
// every track turns an album into a slideshow. The grace is there so a series
// cannot run away from someone across an evening — a real risk for a 45-minute
// episode, not for a four-minute track, where stop is one tap away and the cost
// of a wrong guess is seconds.
//
// Both ends must be audio, not just the finished one. Same-kind chaining
// already guarantees that on the file branch, but the check is spelled out
// rather than inferred: this is the only thing standing between "no countdown"
// and a film starting unannounced, and a future resolver that stops chaining
// same-kind would otherwise silently take the countdown with it.
func autoplayGraceFor(finished player.PlayContext, next *autoplayNext, configured time.Duration) time.Duration {
	// Audio → audio stays exempt whatever the user picked: a countdown card
	// between every track of an album is noise, and this is the one chain that
	// is expected to run unattended for a whole record.
	if next != nil && finished.AudioOnly && next.IsAudio {
		return 0
	}
	return configured
}

func (s *Server) lookupNextEpisode(finished player.PlayContext) (*autoplayNext, error) {
	if s.resolveNextEpisode != nil {
		return s.resolveNextEpisode(finished)
	}
	if config.IsFileServerType(finished.SourceType) {
		return s.lookupNextFile(finished)
	}
	// Prefer the configured server type when SourceType is empty/legacy.
	if srv := config.GetServer(finished.ServerID); srv != nil && config.IsFileServerType(srv.Type) {
		return s.lookupNextFile(finished)
	}
	client, err := provider.FromServer(finished.ServerID)
	if err != nil {
		return nil, err
	}
	// Empty season id = the whole series, ordered season-then-episode by the
	// provider clients. The chain therefore rolls over into the next season
	// instead of stopping at a season boundary, and list-loop wraps around the
	// entire show rather than replaying one season.
	raw, err := client.Episodes(finished.SeriesID, "", 0, maxAutoplayEpisodes, "asc")
	if err != nil {
		return nil, err
	}
	return parseNextEpisode(raw, finished.ItemID, finished.SeriesID, finished.SeasonID, finished.SeriesTitle, finished.ServerID, autoplayLoopEnabled())
}

// lookupNextFile resolves the next video in the finished file's parent
// directory via one ListDir + the pure filesource.NextVideo decision table.
func (s *Server) lookupNextFile(finished player.PlayContext) (*autoplayNext, error) {
	client, err := filesource.FromServer(finished.ServerID)
	if err != nil {
		return nil, err
	}
	// File playback stores the relative path in ItemID (and sometimes Title).
	currentPath := finished.ItemID
	parent := filesource.ParentDir(currentPath)
	listing, err := client.ListDir(parent)
	if err != nil {
		return nil, err
	}
	if len(listing.Entries) > filesource.MaxAutoplayDirEntries {
		log.Printf("autoplay: parent directory has %d entries (cap %d); giving up",
			len(listing.Entries), filesource.MaxAutoplayDirEntries)
		return nil, nil
	}
	loop := autoplayLoopEnabled()
	next, ok, wrapped := filesource.NextVideoWrapping(listing.Entries, currentPath)
	if !ok || (wrapped && !loop) {
		return nil, nil
	}
	// Prefer the finished title (filename) for gap logging; fall back to path.
	currentName := finished.Title
	if currentName == "" {
		currentName = currentPath
	}
	if gap, from, to := filesource.EpisodeGap(currentName, next.Name); gap {
		log.Printf("autoplay: episode gap in folder %q: E%02d → E%02d (%s → %s)",
			parent, from, to, currentPath, next.Path)
	}
	title := filesource.TitleWithoutExtension(next.Name)
	return &autoplayNext{
		ServerID:  finished.ServerID,
		ItemID:    next.Path,
		Title:     title,
		IsFile:    true,
		Path:      next.Path,
		IsAudio:   next.IsAudio,
		Restarted: wrapped,
	}, nil
}

// parseNextEpisode finds the episode after currentItemID in a whole-series
// payload (so season rollover happens naturally). With loop set, the end of
// the series wraps back to its first episode.
func parseNextEpisode(raw []byte, currentItemID, seriesID, seasonID, seriesTitle, serverID string, loop bool) (*autoplayNext, error) {
	var payload struct {
		Items []struct {
			ID                string `json:"Id"`
			Name              string `json:"Name"`
			SeriesID          string `json:"SeriesId"`
			SeasonID          string `json:"SeasonId"`
			SeriesName        string `json:"SeriesName"`
			ParentIndexNumber int    `json:"ParentIndexNumber"`
			IndexNumber       int    `json:"IndexNumber"`
			SeasonName        string `json:"SeasonName"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	idx := -1
	for i, ep := range payload.Items {
		if ep.ID == currentItemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	restarted := false
	nextIdx := idx + 1
	if nextIdx >= len(payload.Items) {
		if !loop {
			return nil, nil
		}
		// Wrapping to itself is the single-episode series case, and is the
		// same "repeat this one title" the movie path gets from mpv.
		nextIdx = 0
		restarted = true
	}
	next := payload.Items[nextIdx]
	nextSeriesID := next.SeriesID
	if nextSeriesID == "" {
		nextSeriesID = seriesID
	}
	nextSeasonID := next.SeasonID
	if nextSeasonID == "" {
		nextSeasonID = seasonID
	}
	label := next.SeasonName
	if next.ParentIndexNumber > 0 && next.IndexNumber > 0 {
		label = fmt.Sprintf("S%02d E%02d", next.ParentIndexNumber, next.IndexNumber)
	}
	seriesName := next.SeriesName
	if seriesName == "" {
		seriesName = seriesTitle
	}
	poster := nextSeriesID
	if poster == "" {
		poster = next.ID
	}
	return &autoplayNext{
		ServerID:     serverID,
		ItemID:       next.ID,
		SeriesID:     nextSeriesID,
		SeasonID:     nextSeasonID,
		Title:        next.Name,
		SeriesTitle:  seriesName,
		EpisodeLabel: label,
		PosterItemID: poster,
		Restarted:    restarted,
	}, nil
}

func (s *Server) fireAutoplay(gen uint64) {
	s.autoplayMu.Lock()
	if gen != s.autoplay.generation || s.autoplay.status != autoplayStatusNextAvailable || s.autoplay.next == nil {
		s.autoplayMu.Unlock()
		return
	}
	next := *s.autoplay.next
	// Consume the pending transition so a second fire cannot double-play.
	if s.autoplayCancel != nil {
		s.autoplayCancel = nil
	}
	s.autoplay.status = ""
	s.autoplay.next = nil
	s.autoplay.deadline = time.Time{}
	s.autoplay.finished = player.PlayContext{}
	s.autoplayMu.Unlock()

	s.noteAutoplayTransition()
	s.startAutoplayNext(next)
}

// PlayAutoplayNow executes the already-resolved pending next episode once.
func (s *Server) PlayAutoplayNow() map[string]any {
	s.autoplayMu.Lock()
	if s.autoplay.status != autoplayStatusNextAvailable || s.autoplay.next == nil {
		s.autoplayMu.Unlock()
		return map[string]any{"ok": false, "error": "No next episode available"}
	}
	next := *s.autoplay.next
	if s.autoplayCancel != nil {
		s.autoplayCancel()
		s.autoplayCancel = nil
	}
	s.autoplay.generation++
	s.autoplay.status = ""
	s.autoplay.next = nil
	s.autoplay.deadline = time.Time{}
	s.autoplay.finished = player.PlayContext{}
	s.autoplayMu.Unlock()

	return s.startAutoplayNext(next)
}

func (s *Server) startAutoplayNext(next autoplayNext) map[string]any {
	if s.playAutoplayNext != nil {
		return s.playAutoplayNext(next)
	}
	generation := s.beginPlay()
	if next.IsFile || next.Path != "" {
		return s.playResolvedFileItem(generation, next)
	}
	return s.playResolvedMediaItem(generation, next)
}

func (s *Server) playResolvedMediaItem(generation uint64, next autoplayNext) map[string]any {
	client, err := provider.FromServer(next.ServerID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	choice, err := client.ChoosePlayURL(next.ItemID, "")
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	opts := mediaAutoplayPlayOpts(next, choice.MediaSourceID, client.ResumePositionSeconds(next.ItemID))
	if srv := config.GetServer(next.ServerID); srv != nil {
		opts.SourceType = config.NormalizeServerType(srv.Type)
	}
	if !s.isCurrentPlay(generation) {
		return map[string]any{"ok": false, "superseded": true}
	}
	result := s.player.Play(choice.URL, opts)
	if ok, _ := result["ok"].(bool); ok && s.isCurrentPlay(generation) {
		client.ReportStart(next.ItemID, s.player.PlaySessionID(), choice.MediaSourceID)
	}
	return result
}

// playResolvedFileItem starts the next folder file. It reuses the same stream
// URL builder and the same stored resume position as manual play. No
// media-server ReportStart.
func (s *Server) playResolvedFileItem(generation uint64, next autoplayNext) map[string]any {
	path := next.Path
	if path == "" {
		path = next.ItemID
	}
	files, err := provider.FileFromServer(next.ServerID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if _, err := files.ResolvePlayURL(path); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	playURL := s.fileStreamPlayURL(next.ServerID, path)
	title := next.Title
	if title == "" {
		title = filesource.TitleWithoutExtension(path)
	}
	opts := fileAutoplayPlayOpts(next.ServerID, path, title)
	if srv := config.GetServer(next.ServerID); srv != nil {
		opts.SourceType = config.NormalizeServerType(srv.Type)
	}
	// Same audio presentation as a manual play of this path — autoplay must
	// not drop cover art / force-window for the next track.
	applyFileAudioPresentation(&opts, path, s.port)
	if !s.isCurrentPlay(generation) {
		return map[string]any{"ok": false, "superseded": true}
	}
	return s.player.Play(playURL, opts)
}

// mediaAutoplayPlayOpts and fileAutoplayPlayOpts take the same saved position a
// manual play of the next title would use. An automatic transition is not a
// reason to discard a genuine half-watched bookmark — that is the one case a
// person notices, and it is the only case where this choice is even visible:
// a next title nobody has opened has no position, and one that was already
// finished is sent back to 0 by the tail rule in the resume lookups. Without
// that rule this would resume at the tail, flash a frame and chain onwards,
// which is why autoplay used to pass 0 unconditionally.
func mediaAutoplayPlayOpts(next autoplayNext, mediaSourceID string, startSeconds float64) player.PlayOptions {
	return playOpts(next.ServerID, next.ItemID, next.SeriesID, next.SeasonID,
		next.Title, next.SeriesTitle, next.EpisodeLabel, next.PosterItemID, startSeconds, mediaSourceID)
}

func fileAutoplayPlayOpts(serverID, path, title string) player.PlayOptions {
	return playOpts(serverID, path, "", "", title, "", "", "",
		config.LocalPlaybackPosition(serverID, path), "")
}

// mergeAutoplayState overlays host autoplay fields onto player state.
func (s *Server) mergeAutoplayState(state map[string]any) map[string]any {
	s.autoplayMu.Lock()
	status := s.autoplay.status
	deadline := s.autoplay.deadline
	silent := s.autoplay.silent
	var next *autoplayNext
	if s.autoplay.next != nil {
		cp := *s.autoplay.next
		next = &cp
	}
	s.autoplayMu.Unlock()

	if status == "" || silent {
		return state
	}
	state["autoplay_status"] = status
	if !deadline.IsZero() {
		remaining := deadline.Sub(s.now())
		if remaining < 0 {
			remaining = 0
		}
		state["autoplay_deadline_ms"] = deadline.UnixMilli()
		state["autoplay_remaining_ms"] = remaining.Milliseconds()
	}
	if next != nil {
		title := next.Title
		if next.EpisodeLabel != "" {
			title = next.EpisodeLabel + " " + next.Title
		}
		state["next_episode_title"] = title
		state["next_episode_label"] = next.EpisodeLabel
		state["next_episode_id"] = next.ItemID
		if next.Restarted {
			// The list wrapped; the phone says "starting over" instead of
			// "up next", which is the only difference this makes.
			state["autoplay_restarted"] = true
		}
	}
	return state
}

// autoplaySnapshot is exposed for tests.
func (s *Server) autoplaySnapshot() autoplayState {
	s.autoplayMu.Lock()
	defer s.autoplayMu.Unlock()
	cp := s.autoplay
	if s.autoplay.next != nil {
		n := *s.autoplay.next
		cp.next = &n
	}
	return cp
}
