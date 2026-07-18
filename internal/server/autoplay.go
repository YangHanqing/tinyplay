package server

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/player"
	"tvremote/internal/provider"
)

// Host-owned next-episode autoplay. The phone UI may display the countdown
// and issue play-now / cancel, but must never own the timer or next selection:
// a backgrounded or closed browser must still advance playback.

const autoplayGrace = 5 * time.Second

// autoplay status values exposed on GET /api/player/state. There is no
// "finished" status: when no transition will run, the completed context is
// cleared instead so the state does not advertise the finished episode forever.
const (
	autoplayStatusFindingNext   = "finding_next"
	autoplayStatusNextAvailable = "next_available"
)

type autoplayNext struct {
	ServerID     string
	ItemID       string
	SeriesID     string
	SeasonID     string
	Title        string
	SeriesTitle  string
	EpisodeLabel string
	PosterItemID string
}

type autoplayState struct {
	generation uint64
	status     string
	deadline   time.Time
	finished   player.PlayContext
	next       *autoplayNext
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
	s.autoplayMu.Unlock()
	if clearCompleted {
		s.player.ClearCompletedPlayback()
	}
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
// host autoplay at all: a media-server series episode with the setting on.
func autoplayEligible(ctx player.PlayContext) bool {
	if !ctx.PlaybackCompleted || ctx.ItemID == "" || ctx.SeriesID == "" {
		return false
	}
	// IPTV / DLNA / file sources never autoplay here — only media-server series.
	if ctx.IsLive || ctx.SourceType == "iptv" || ctx.SourceType == "iptv-catchup" ||
		ctx.SourceType == "dlna" || config.IsFileServerType(ctx.SourceType) {
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
	s.autoplay.next = next
	s.autoplay.status = autoplayStatusNextAvailable
	s.autoplay.deadline = s.now().Add(autoplayGrace)
	s.autoplayMu.Unlock()

	s.scheduleAfter(autoplayGrace, gen, func() {
		s.fireAutoplay(gen)
	})
}

func (s *Server) lookupNextEpisode(finished player.PlayContext) (*autoplayNext, error) {
	if s.resolveNextEpisode != nil {
		return s.resolveNextEpisode(finished)
	}
	client, err := provider.FromServer(finished.ServerID)
	if err != nil {
		return nil, err
	}
	raw, err := client.Episodes(finished.SeriesID, finished.SeasonID, 0, 200, "asc")
	if err != nil {
		return nil, err
	}
	return parseNextEpisode(raw, finished.ItemID, finished.SeriesID, finished.SeasonID, finished.SeriesTitle, finished.ServerID)
}

// parseNextEpisode finds the episode after currentItemID within the same
// season payload. Season rollover is intentionally not performed.
func parseNextEpisode(raw []byte, currentItemID, seriesID, seasonID, seriesTitle, serverID string) (*autoplayNext, error) {
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
	if idx < 0 || idx+1 >= len(payload.Items) {
		return nil, nil
	}
	next := payload.Items[idx+1]
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
	// A partially-watched next episode still honours Emby resume (same as a
	// manual play of that item).
	startSeconds := client.ResumePositionSeconds(next.ItemID)
	opts := playOpts(next.ServerID, next.ItemID, next.SeriesID, next.SeasonID,
		next.Title, next.SeriesTitle, next.EpisodeLabel, next.PosterItemID, startSeconds, choice.MediaSourceID)
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

// mergeAutoplayState overlays host autoplay fields onto player state.
func (s *Server) mergeAutoplayState(state map[string]any) map[string]any {
	s.autoplayMu.Lock()
	status := s.autoplay.status
	deadline := s.autoplay.deadline
	var next *autoplayNext
	if s.autoplay.next != nil {
		cp := *s.autoplay.next
		next = &cp
	}
	s.autoplayMu.Unlock()

	if status == "" {
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
