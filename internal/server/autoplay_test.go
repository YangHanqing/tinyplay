package server

import (
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

func TestParseNextEpisodeSameSeasonOnly(t *testing.T) {
	raw := []byte(`{"Items":[
		{"Id":"e1","Name":"One","SeriesId":"s","SeasonId":"sea","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":1},
		{"Id":"e2","Name":"Two","SeriesId":"s","SeasonId":"sea","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":2}
	]}`)
	next, err := parseNextEpisode(raw, "e1", "s", "sea", "Show", "srv")
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ItemID != "e2" || next.Title != "Two" || next.EpisodeLabel != "S01 E02" {
		t.Fatalf("unexpected next: %+v", next)
	}

	last, err := parseNextEpisode(raw, "e2", "s", "sea", "Show", "srv")
	if err != nil {
		t.Fatal(err)
	}
	if last != nil {
		t.Fatalf("expected no next after last episode, got %+v", last)
	}

	missing, err := parseNextEpisode(raw, "missing", "s", "sea", "Show", "srv")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil for stale item id, got %+v", missing)
	}
}

func TestParseNextEpisodeWithoutSeasonContext(t *testing.T) {
	raw := []byte(`{"Items":[{"Id":"e1","SeriesId":"s"},{"Id":"e2","Name":"Two","SeriesId":"s","SeasonId":"sea"}]}`)
	next, err := parseNextEpisode(raw, "e1", "s", "", "Show", "srv")
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
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		if d != autoplayGrace {
			t.Fatalf("grace duration = %v, want %v", d, autoplayGrace)
		}
		fireFn = fn
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
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
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
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
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
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
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
	s.autoplayAfter = func(d time.Duration, fn func()) func() {
		fireFn = fn
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
