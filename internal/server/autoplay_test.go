package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

func setupAutoplayTest(t *testing.T) (*Server, string) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	srv := config.AddServer(config.Server{
		Name: "Test Emby", Type: "emby", Protocol: "http",
		Hosts: []string{"127.0.0.1"}, Port: 8096,
	})
	p := player.New()
	s := New(p)
	return s, srv.ID
}

func TestParseNextEpisodeStopsAtTheEndOfTheList(t *testing.T) {
	raw := []byte(`{"Items":[
		{"Id":"e1","Name":"One","SeriesId":"s","SeasonId":"sea","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":1},
		{"Id":"e2","Name":"Two","SeriesId":"s","SeasonId":"sea","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":2}
	]}`)
	next, err := parseNextEpisode(raw, "e1", "s", "sea", "Show", "srv", false)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ItemID != "e2" || next.Title != "Two" || next.EpisodeLabel != "S01 E02" {
		t.Fatalf("unexpected next: %+v", next)
	}

	last, err := parseNextEpisode(raw, "e2", "s", "sea", "Show", "srv", false)
	if err != nil {
		t.Fatal(err)
	}
	if last != nil {
		t.Fatalf("expected no next after last episode, got %+v", last)
	}

	missing, err := parseNextEpisode(raw, "missing", "s", "sea", "Show", "srv", false)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil for stale item id, got %+v", missing)
	}
}

func TestParseNextEpisodeWithoutSeasonContext(t *testing.T) {
	raw := []byte(`{"Items":[{"Id":"e1","SeriesId":"s"},{"Id":"e2","Name":"Two","SeriesId":"s","SeasonId":"sea"}]}`)
	next, err := parseNextEpisode(raw, "e1", "s", "", "Show", "srv", false)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ItemID != "e2" || next.SeasonID != "sea" {
		t.Fatalf("unexpected next without season context: %+v", next)
	}
}

func TestAutoplayGraceTimerFiresWithoutBrowser(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var played atomic.Value
	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		if d != autoplayGrace {
			t.Fatalf("grace duration = %v, want %v", d, autoplayGrace)
		}
		fireFn = fn
		armed <- struct{}{}
		return func() { fireFn = nil }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{
			ServerID: finished.ServerID, ItemID: "e2", SeriesID: finished.SeriesID,
			SeasonID: finished.SeasonID, Title: "Two", SeriesTitle: "Show",
			EpisodeLabel: "S01 E02", PosterItemID: finished.SeriesID,
		}, nil
	}
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		played.Store(next.ItemID)
		return map[string]any{"ok": true}
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		Title: "One", SeriesTitle: "Show", PlaybackCompleted: true, SourceType: "emby",
	}, 1)

	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)
	if fireFn == nil {
		t.Fatal("timer was not armed")
	}
	fireFn()
	if got, _ := played.Load().(string); got != "e2" {
		t.Fatalf("played item = %q, want e2", got)
	}
	if s.autoplaySnapshot().status != "" {
		t.Fatalf("pending autoplay not cleared after fire: %+v", s.autoplaySnapshot())
	}
}

func TestAutoplayCancelPreventsFire(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var cancelled bool
	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
		armed <- struct{}{}
		return func() { cancelled = true; fireFn = nil }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{ServerID: serverID, ItemID: "e2", SeriesID: "ser", SeasonID: "sea", Title: "Two"}, nil
	}
	var plays int32
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		atomic.AddInt32(&plays, 1)
		return map[string]any{"ok": true}
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)

	genBefore := s.autoplaySnapshot().generation
	s.CancelAutoplay(true)
	if !cancelled {
		t.Fatal("timer cancel was not invoked")
	}
	if s.autoplaySnapshot().status != "" {
		t.Fatal("status should be cleared after cancel")
	}
	if fireFn != nil {
		t.Fatal("cancel should clear scheduled callback handle")
	}
	s.fireAutoplay(genBefore)
	if atomic.LoadInt32(&plays) != 0 {
		t.Fatal("stale fire after cancel started playback")
	}
}

func TestAutoplayPlayNowOnceAndSupersede(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
		armed <- struct{}{}
		return func() { fireFn = nil }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{ServerID: serverID, ItemID: "e2", SeriesID: "ser", SeasonID: "sea", Title: "Two"}, nil
	}
	var plays int32
	var mu sync.Mutex
	var last string
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		atomic.AddInt32(&plays, 1)
		mu.Lock()
		last = next.ItemID
		mu.Unlock()
		return map[string]any{"ok": true}
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)

	result := s.PlayAutoplayNow()
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("play now failed: %+v", result)
	}
	if atomic.LoadInt32(&plays) != 1 || last != "e2" {
		t.Fatalf("plays=%d last=%q", plays, last)
	}
	result = s.PlayAutoplayNow()
	if ok, _ := result["ok"].(bool); ok {
		t.Fatal("second play-now should fail")
	}
	if fireFn != nil {
		fireFn()
	}
	if atomic.LoadInt32(&plays) != 1 {
		t.Fatalf("expected single play, got %d", plays)
	}
}

func TestNewPlaySupersedesPendingAutoplay(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var cancelled bool
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		armed <- struct{}{}
		return func() { cancelled = true }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{ServerID: serverID, ItemID: "e2", SeriesID: "ser", SeasonID: "sea", Title: "Two"}, nil
	}
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		t.Fatal("should not autoplay after superseding play")
		return nil
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)

	s.CancelAutoplay(false) // same path playItem uses before beginPlay
	if !cancelled {
		t.Fatal("expected timer cancel on superseding play")
	}
	if s.autoplaySnapshot().status != "" {
		t.Fatal("pending autoplay survived superseding play")
	}
}

func TestAutoplaySkipsNonSeriesAndDisabled(t *testing.T) {
	s, serverID := setupAutoplayTest(t)
	called := false
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		called = true
		return nil, nil
	}
	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "movie1", PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	time.Sleep(20 * time.Millisecond)
	if called {
		t.Fatal("lookup should not run for non-series completion")
	}

	config.SetAutoplayNextEpisode(false)
	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 2)
	time.Sleep(20 * time.Millisecond)
	if called {
		t.Fatal("lookup should not run when autoplay is disabled")
	}
	if s.autoplaySnapshot().status != "" {
		t.Fatal("autoplay should stay idle when disabled")
	}
}

func TestAutoplayEligibleFileSource(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	fileSrv := config.AddServer(config.Server{
		Name: "Local Movies", Type: "local", RootPath: data,
	})
	ctx := player.PlayContext{
		ServerID: fileSrv.ID, ItemID: "Show/S01E01.mkv",
		PlaybackCompleted: true, SourceType: "local",
	}
	if !autoplayEligible(ctx) {
		t.Fatal("file source with path ItemID should be autoplay-eligible")
	}
	ctx.ItemID = ""
	if autoplayEligible(ctx) {
		t.Fatal("file source without path must not be eligible")
	}
	// IPTV still rejected even with an ItemID.
	iptv := config.AddServer(config.Server{Name: "IPTV", Type: "iptv", Hosts: []string{"x"}})
	if autoplayEligible(player.PlayContext{
		ServerID: iptv.ID, ItemID: "ch1", PlaybackCompleted: true, SourceType: "iptv",
	}) {
		t.Fatal("iptv must not be autoplay-eligible")
	}
}

func TestLookupNextFileNFSUsesNaturalFilenameOrder(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", filepath.Join(data, "config"))
	media := filepath.Join(data, "media")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01.mkv", "02.mkv", "10.mkv", "subtitle.srt", "poster.jpg"} {
		if err := os.WriteFile(filepath.Join(media, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fileSrv := config.AddServer(config.Server{Name: "Mounted NFS", Type: "nfs", RootPath: media})
	s := New(player.New())

	next, err := s.lookupNextFile(player.PlayContext{
		ServerID: fileSrv.ID, ItemID: "01.mkv", Title: "01", SourceType: "nfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || !next.IsFile || next.Path != "02.mkv" {
		t.Fatalf("NFS next = %+v, want 02.mkv", next)
	}

	next, err = s.lookupNextFile(player.PlayContext{
		ServerID: fileSrv.ID, ItemID: "02.mkv", Title: "02", SourceType: "nfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.Path != "10.mkv" {
		t.Fatalf("NFS next = %+v, want 10.mkv", next)
	}
}

// Autoplay carries the next title's saved position through, exactly as a
// manual play of it would. The tail rule inside the resume lookups is what
// keeps a finished next title at 0; autoplay itself no longer second-guesses
// the bookmark.
func TestAutoplayPlayOptionsCarryTheSavedPosition(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())

	media := mediaAutoplayPlayOpts(autoplayNext{
		ServerID: "emby", ItemID: "e2", SeriesID: "series", SeasonID: "season",
		Title: "Two",
	}, "source", 930)
	if media.StartSeconds != 930 {
		t.Fatalf("media autoplay start = %v, want the saved 930", media.StartSeconds)
	}

	const oneHour = 3600.0
	config.RecordLocalPlayback("nfs", "Season/02.mkv", 930, oneHour)
	file := fileAutoplayPlayOpts("nfs", "Season/02.mkv", "02")
	if file.StartSeconds != 930 {
		t.Fatalf("file autoplay start = %v, want the saved 930", file.StartSeconds)
	}
}

// A next title that was already watched to the end must still start at 0 —
// otherwise autoplay opens it at the tail, reaches EOF immediately and chains
// on through the folder.
func TestAutoplayFileStartsFinishedNextTitleFromZero(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())

	const oneHour = 3600.0
	config.RecordLocalPlayback("nfs", "Season/02.mkv", oneHour-5, oneHour)
	if got := fileAutoplayPlayOpts("nfs", "Season/02.mkv", "02").StartSeconds; got != 0 {
		t.Fatalf("file autoplay start = %v, want 0 for a finished next title", got)
	}

	// And a title nobody has opened has nothing to carry.
	if got := fileAutoplayPlayOpts("nfs", "Season/03.mkv", "03").StartSeconds; got != 0 {
		t.Fatalf("file autoplay start = %v, want 0 for an unwatched next title", got)
	}
}

func TestAutoplayFileBranchPlaysResolvedPath(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	fileSrv := config.AddServer(config.Server{
		Name: "Local", Type: "local", RootPath: data,
	})
	s := New(player.New())

	var played atomic.Value
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{
			ServerID: finished.ServerID,
			ItemID:   "Show/S01E02.mkv",
			Path:     "Show/S01E02.mkv",
			Title:    "S01E02",
			IsFile:   true,
		}, nil
	}
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		played.Store(next)
		return map[string]any{"ok": true}
	}
	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
		armed <- struct{}{}
		return func() { fireFn = nil }
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: fileSrv.ID, ItemID: "Show/S01E01.mkv", Title: "S01E01",
		PlaybackCompleted: true, SourceType: "local",
	}, 1)
	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	snap := s.autoplaySnapshot()
	if snap.next == nil || !snap.next.IsFile || snap.next.Path != "Show/S01E02.mkv" {
		t.Fatalf("expected file next, got %+v", snap.next)
	}
	if snap.next.Title != "S01E02" || snap.next.EpisodeLabel != "" {
		t.Fatalf("file next title/label = %q / %q", snap.next.Title, snap.next.EpisodeLabel)
	}
	waitAutoplayTimer(t, armed)
	fireFn()
	got, _ := played.Load().(autoplayNext)
	if !got.IsFile || got.Path != "Show/S01E02.mkv" {
		t.Fatalf("played = %+v", got)
	}
}

func TestDelayedEOFAfterExplicitCancelIsRejected(t *testing.T) {
	s, serverID := setupAutoplayTest(t)
	called := false
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		called = true
		return &autoplayNext{ServerID: serverID, ItemID: "e2"}, nil
	}

	// At revision 0, an explicit play/stop/cancel also invalidates the terminal
	// cleanup revision (1) that the old process may be about to publish.
	s.CancelAutoplay(false)
	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	time.Sleep(20 * time.Millisecond)

	if called || s.autoplaySnapshot().status != "" {
		t.Fatal("late EOF callback recreated autoplay after explicit cancellation")
	}
}

// Every other test here calls onNaturalEOF directly, which leaves the hand-off
// from the player unguarded — the feature can be fully dead while they all pass.
// Drive the reporter the way the player's process waiter does instead.
func TestPlayerNaturalEOFReporterDrivesAutoplay(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
		armed <- struct{}{}
		return func() { fireFn = nil }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{ServerID: finished.ServerID, ItemID: "e2", Title: "Two"}, nil
	}
	var played atomic.Value
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		played.Store(next.ItemID)
		return map[string]any{"ok": true}
	}

	reporter := s.player.NaturalEOFReporter
	if reporter == nil {
		t.Fatal("player has no NaturalEOFReporter: autoplay is never triggered in production")
	}
	reporter(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		Title: "One", SeriesTitle: "Show", PlaybackCompleted: true, SourceType: "emby",
	}, 1)

	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)
	if fireFn == nil {
		t.Fatal("grace timer was not armed from the player callback")
	}
	fireFn()
	if got, _ := played.Load().(string); got != "e2" {
		t.Fatalf("played item = %q, want e2", got)
	}
}

func TestMergeAutoplayStateExposesDeadline(t *testing.T) {
	p := player.New()
	s := New(p)
	fixed := time.UnixMilli(1_700_000_000_000)
	s.autoplayNow = func() time.Time { return fixed }
	s.autoplayMu.Lock()
	s.autoplay.status = autoplayStatusNextAvailable
	s.autoplay.deadline = fixed.Add(3 * time.Second)
	s.autoplay.next = &autoplayNext{ItemID: "e2", Title: "Two", EpisodeLabel: "S01 E02"}
	s.autoplayMu.Unlock()

	state := s.mergeAutoplayState(map[string]any{"item_id": "e1"})
	if state["autoplay_status"] != autoplayStatusNextAvailable {
		t.Fatalf("status = %v", state["autoplay_status"])
	}
	if state["autoplay_deadline_ms"] != fixed.Add(3*time.Second).UnixMilli() {
		t.Fatalf("deadline = %v", state["autoplay_deadline_ms"])
	}
	if state["autoplay_remaining_ms"] != int64(3000) {
		t.Fatalf("remaining = %v", state["autoplay_remaining_ms"])
	}
	if state["next_episode_title"] != "S01 E02 Two" {
		t.Fatalf("title = %v", state["next_episode_title"])
	}
}

func waitAutoplayStatus(t *testing.T, s *Server, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.autoplaySnapshot().status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for autoplay status %q (got %q)", want, s.autoplaySnapshot().status)
}

func waitAutoplayTimer(t *testing.T, armed <-chan struct{}) {
	t.Helper()
	select {
	case <-armed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for autoplay timer to arm")
	}
}

// A cancel that lands while a title is playing must not blacklist that title's
// own completion. Nothing bumps the revision between such a cancel and the
// playing title's EOF, so blocking revision+1 there would leave the episode on
// screen with autoplay permanently dead.
func TestAutoplayBlockThroughSparesPlayingTitle(t *testing.T) {
	if got := autoplayBlockThrough(5, true); got != 5 {
		t.Fatalf("blockThrough(5, playing) = %d, want 5: ep5's own EOF at revision 6 must survive", got)
	}
	// Idle: a terminal cleanup may be in flight and will report revision+1.
	if got := autoplayBlockThrough(5, false); got != 6 {
		t.Fatalf("blockThrough(5, idle) = %d, want 6: a racing EOF callback must be swallowed", got)
	}
	if got := autoplayBlockThrough(^uint64(0), false); got != ^uint64(0) {
		t.Fatalf("blockThrough(max, idle) = %d, want no overflow", got)
	}
}

// The player keeps the finished-episode context only for host autoplay. When
// no transition will run, it must be dropped immediately — otherwise
// /api/player/state advertises the finished episode forever and the phone
// remote repaints it as a permanently "starting" card.
func TestCompletedContextClearedWhenAutoplayDisabled(t *testing.T) {
	s, serverID := setupAutoplayTest(t)
	var cleared atomic.Int32
	s.clearCompleted = func() { cleared.Add(1) }

	config.SetAutoplayNextEpisode(false)
	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)
	if cleared.Load() != 1 {
		t.Fatalf("completed context cleared %d times, want 1", cleared.Load())
	}
	if s.autoplaySnapshot().status != "" {
		t.Fatal("autoplay must stay idle when disabled")
	}
}

func TestCompletedContextClearedWhenNoNextEpisode(t *testing.T) {
	s, serverID := setupAutoplayTest(t)
	var cleared atomic.Int32
	s.clearCompleted = func() { cleared.Add(1) }
	s.resolveNextEpisode = func(player.PlayContext) (*autoplayNext, error) {
		return nil, nil
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cleared.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if cleared.Load() != 1 {
		t.Fatalf("completed context cleared %d times, want 1", cleared.Load())
	}
	if snap := s.autoplaySnapshot(); snap.status != "" || snap.next != nil {
		t.Fatalf("season-over must leave no sticky autoplay state, got %+v", snap)
	}
}

func TestCompletedContextRetainedWhileNextEpisodePending(t *testing.T) {
	s, serverID := setupAutoplayTest(t)
	var cleared atomic.Int32
	s.clearCompleted = func() { cleared.Add(1) }
	s.autoplayAfter = func(d time.Duration, fn func()) func() { return func() {} }
	s.resolveNextEpisode = func(player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{ServerID: serverID, ItemID: "e2"}, nil
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "e1", SeriesID: "ser", SeasonID: "sea",
		PlaybackCompleted: true, SourceType: "emby",
	}, 1)

	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	if cleared.Load() != 0 {
		t.Fatal("completed context must survive while the countdown is pending")
	}
}

// A track into another track skips the countdown: a five-second "up next" card
// between every song turns an album into a slideshow. The grace protects a long
// episode from running away with someone's evening, which a four-minute track
// does not need.
func TestAutoplayGraceForSkipsCountdownBetweenTracks(t *testing.T) {
	song := player.PlayContext{AudioOnly: true}
	film := player.PlayContext{}
	nextTrack := &autoplayNext{IsFile: true, IsAudio: true}
	nextFilm := &autoplayNext{IsFile: true}

	if got := autoplayGraceFor(song, nextTrack, autoplayGrace); got != 0 {
		t.Errorf("song → song grace = %v, want 0", got)
	}
	// Both ends must be audio. Same-kind chaining already guarantees it, but
	// this check is the only thing standing between "no countdown" and a film
	// starting unannounced, so it is asserted rather than assumed.
	if got := autoplayGraceFor(song, nextFilm, autoplayGrace); got != autoplayGrace {
		t.Errorf("song → film grace = %v, want %v", got, autoplayGrace)
	}
	if got := autoplayGraceFor(film, nextTrack, autoplayGrace); got != autoplayGrace {
		t.Errorf("film → track grace = %v, want %v", got, autoplayGrace)
	}
	if got := autoplayGraceFor(film, nextFilm, autoplayGrace); got != autoplayGrace {
		t.Errorf("film → film grace = %v, want %v", got, autoplayGrace)
	}
	if got := autoplayGraceFor(song, nil, autoplayGrace); got != autoplayGrace {
		t.Errorf("nil next grace = %v, want %v", got, autoplayGrace)
	}
}

// A zero-grace transition must publish no autoplay fields at all. Advertising
// next_available for even one poll flashes the card this exists to remove, and
// the phone draws a full five-second bar when a status arrives without a
// remaining time — so the flash would be the longest-looking one possible.
func TestSilentAutoplayPublishesNoCountdownFields(t *testing.T) {
	p := player.New()
	s := New(p)
	s.autoplayMu.Lock()
	s.autoplay.status = autoplayStatusNextAvailable
	s.autoplay.next = &autoplayNext{ItemID: "02.flac", Title: "Two", IsFile: true, IsAudio: true}
	s.autoplay.silent = true
	s.autoplayMu.Unlock()

	state := s.mergeAutoplayState(map[string]any{"item_id": "01.flac"})
	for _, key := range []string{"autoplay_status", "autoplay_deadline_ms", "autoplay_remaining_ms", "next_episode_title"} {
		if _, ok := state[key]; ok {
			t.Errorf("silent transition published %q = %v", key, state[key])
		}
	}
	if state["item_id"] != "01.flac" {
		t.Fatalf("player state was disturbed: %v", state["item_id"])
	}
}

// End to end on the file branch: finishing a track arms a zero-duration timer
// and the next track is played, with nothing for the phone to render meanwhile.
func TestAudioAutoplayArmsZeroGrace(t *testing.T) {
	s, serverID := setupAutoplayTest(t)

	var played atomic.Value
	var fireFn func()
	armed := make(chan struct{}, 1)
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		if d != 0 {
			t.Errorf("grace duration = %v, want 0 for track → track", d)
		}
		fireFn = fn
		armed <- struct{}{}
		return func() { fireFn = nil }
	}
	s.resolveNextEpisode = func(finished player.PlayContext) (*autoplayNext, error) {
		return &autoplayNext{
			ServerID: finished.ServerID, ItemID: "Album/02.flac", Title: "Two",
			IsFile: true, Path: "Album/02.flac", IsAudio: true,
		}, nil
	}
	s.playAutoplayNext = func(next autoplayNext) map[string]any {
		played.Store(next.ItemID)
		return map[string]any{"ok": true}
	}

	s.onNaturalEOF(player.PlayContext{
		ServerID: serverID, ItemID: "Album/01.flac", Title: "One",
		PlaybackCompleted: true, SourceType: "local", AudioOnly: true,
	}, 1)

	waitAutoplayStatus(t, s, autoplayStatusNextAvailable)
	waitAutoplayTimer(t, armed)
	if got := s.mergeAutoplayState(map[string]any{})["autoplay_status"]; got != nil {
		t.Fatalf("countdown advertised for a track transition: %v", got)
	}
	fireFn()
	if got, _ := played.Load().(string); got != "Album/02.flac" {
		t.Fatalf("played item = %q, want Album/02.flac", got)
	}
}

// List-loop wraps the whole show, not one season: the payload is fetched with
// an empty season id, so the last episode of the last season is followed by
// the first episode of the first one.
func TestParseNextEpisodeWrapsTheWholeSeries(t *testing.T) {
	raw := []byte(`{"Items":[
		{"Id":"e1","Name":"One","SeriesId":"s","SeasonId":"sea1","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":1},
		{"Id":"e2","Name":"Two","SeriesId":"s","SeasonId":"sea1","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":2},
		{"Id":"e3","Name":"Three","SeriesId":"s","SeasonId":"sea2","SeriesName":"Show","ParentIndexNumber":2,"IndexNumber":1}
	]}`)

	// Season rollover happens with or without loop: the list is the series.
	rollover, err := parseNextEpisode(raw, "e2", "s", "sea1", "Show", "srv", false)
	if err != nil {
		t.Fatal(err)
	}
	if rollover == nil || rollover.ItemID != "e3" || rollover.SeasonID != "sea2" || rollover.Restarted {
		t.Fatalf("season rollover = %+v, want e3 in sea2 without a restart flag", rollover)
	}

	if last, err := parseNextEpisode(raw, "e3", "s", "sea2", "Show", "srv", false); err != nil || last != nil {
		t.Fatalf("end of series without loop = %+v (err %v), want nil", last, err)
	}

	wrapped, err := parseNextEpisode(raw, "e3", "s", "sea2", "Show", "srv", true)
	if err != nil {
		t.Fatal(err)
	}
	if wrapped == nil || wrapped.ItemID != "e1" || !wrapped.Restarted {
		t.Fatalf("end of series with loop = %+v, want e1 flagged as restarted", wrapped)
	}
}

// A one-episode series is the same "repeat this title" case a lone file gets.
func TestParseNextEpisodeLoopRepeatsALoneEpisode(t *testing.T) {
	raw := []byte(`{"Items":[{"Id":"e1","Name":"Only","SeriesId":"s","SeasonId":"sea"}]}`)
	next, err := parseNextEpisode(raw, "e1", "s", "sea", "Show", "srv", true)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ItemID != "e1" || !next.Restarted {
		t.Fatalf("lone episode with loop = %+v, want itself flagged as restarted", next)
	}
}

// The countdown length is the user's, but audio→audio stays exempt at every
// setting: a card between every track is what that exemption exists to avoid.
func TestAutoplayGraceForHonoursTheConfiguredCountdown(t *testing.T) {
	song := player.PlayContext{AudioOnly: true}
	film := player.PlayContext{}
	nextTrack := &autoplayNext{IsFile: true, IsAudio: true}
	nextFilm := &autoplayNext{IsFile: true}

	for _, configured := range []time.Duration{0, 5 * time.Second, 10 * time.Second} {
		if got := autoplayGraceFor(film, nextFilm, configured); got != configured {
			t.Errorf("film grace at %v = %v, want %v", configured, got, configured)
		}
		if got := autoplayGraceFor(song, nextTrack, configured); got != 0 {
			t.Errorf("track grace at %v = %v, want 0", configured, got)
		}
	}
}

// A loop cannot run out of list, so a folder whose files all die on open would
// relaunch mpv forever. Enough consecutive instant endings abandon the chain.
func TestLoopRunawayGuardStopsARelaunchStorm(t *testing.T) {
	s, _ := setupAutoplayTest(t)
	config.SetAutoplayLoopList(true)

	now := time.Now()
	s.autoplayNow = func() time.Time { return now }

	for i := 1; i <= loopRunawayLimit; i++ {
		s.noteAutoplayTransition()
		now = now.Add(time.Second) // the "play" lasted a second
		tripped := s.loopRunawayTripped()
		if i < loopRunawayLimit && tripped {
			t.Fatalf("tripped after %d short plays, want %d", i, loopRunawayLimit)
		}
		if i == loopRunawayLimit && !tripped {
			t.Fatalf("did not trip after %d short plays", loopRunawayLimit)
		}
	}

	// One ordinary-length play is someone actually watching: the count clears.
	s.noteAutoplayTransition()
	now = now.Add(2 * loopRunawayShortPlay)
	if s.loopRunawayTripped() {
		t.Fatal("a full-length play must not trip the guard")
	}
	for i := 0; i < loopRunawayLimit-1; i++ {
		s.noteAutoplayTransition()
		now = now.Add(time.Second)
		if s.loopRunawayTripped() {
			t.Fatal("the counter did not reset after a full-length play")
		}
	}
}

// With loop off the guard never fires: a linear chain ends by itself, and
// stopping a folder of short clips early would be a regression.
func TestLoopRunawayGuardIgnoresLinearChains(t *testing.T) {
	s, _ := setupAutoplayTest(t)
	config.SetAutoplayLoopList(false)

	now := time.Now()
	s.autoplayNow = func() time.Time { return now }
	for i := 0; i < loopRunawayLimit*2; i++ {
		s.noteAutoplayTransition()
		now = now.Add(time.Second)
		if s.loopRunawayTripped() {
			t.Fatal("linear autoplay must never trip the loop guard")
		}
	}
}

// The phone can only offer 0/5/10, but the API is on the LAN and must not
// take an arbitrary number on trust. 0 is a real choice and has to survive.
func TestSettingsAcceptAutoplayCountdownAndLoop(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := testHandler(New(player.New()))

	put := func(body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPut, "/api/settings", body))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT settings %d: %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := put(`{"autoplay_countdown_secs":0,"autoplay_loop_list":true}`)
	if out["autoplay_countdown_secs"] != float64(0) {
		t.Fatalf("countdown = %v, want 0", out["autoplay_countdown_secs"])
	}
	if out["autoplay_loop_list"] != true {
		t.Fatalf("loop = %v, want true", out["autoplay_loop_list"])
	}

	out = put(`{"autoplay_countdown_secs":7}`)
	if out["autoplay_countdown_secs"] != float64(config.DefaultAutoplayCountdownSecs) {
		t.Fatalf("out-of-range countdown = %v, want the default", out["autoplay_countdown_secs"])
	}
	if out["autoplay_loop_list"] != true {
		t.Fatal("an unrelated settings write must not clear the loop toggle")
	}
}

// Loop for a movie is mpv's own repeat, not a host transition: there is no
// list to advance through, and a relaunch would blank the screen every pass.
func TestLoopSingleTitleOnlyCoversItemsWithNoList(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())

	config.SetAutoplayNextEpisode(true)
	config.SetAutoplayLoopList(true)
	if !loopSingleTitle("") {
		t.Fatal("a movie must repeat in mpv when list-loop is on")
	}
	if loopSingleTitle("series-1") {
		t.Fatal("an episode must chain through its series, not repeat itself")
	}

	config.SetAutoplayLoopList(false)
	if loopSingleTitle("") {
		t.Fatal("no loop setting, no repeat")
	}

	// Loop is a sub-option: with autoplay off nothing repeats either.
	config.SetAutoplayLoopList(true)
	config.SetAutoplayNextEpisode(false)
	if loopSingleTitle("") {
		t.Fatal("autoplay off must disable the loop entirely")
	}
}
