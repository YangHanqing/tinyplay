package player

import "testing"

// newStartedPlayer returns a player in the state Play() leaves behind: a
// starting attempt armed, waiting for mpv to confirm playback.
func newStartedPlayer() *Player {
	p := &Player{liveProps: map[string]any{}}
	p.mu.Lock()
	p.armStartingLocked()
	p.mu.Unlock()
	return p
}

func startingFlag(p *Player) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starting
}

// The fast path: the event arrives and ends the attempt.
func TestPlaybackRestartEndsStartingAttempt(t *testing.T) {
	p := newStartedPlayer()
	if !startingFlag(p) {
		t.Fatal("armStartingLocked did not set starting")
	}
	p.handleEvent([]byte(`{"event":"playback-restart"}`))
	if startingFlag(p) {
		t.Fatal("starting still set after playback-restart")
	}
}

// The case that actually shipped the bug. mpv events are one-shot and reach
// only already-connected clients, and propReaderRun subscribes on a 500ms
// retry cadence after the process is flagged running — so a title that reaches
// playback before that lands never delivers "playback-restart" at all. Roughly
// 1 in 10 cold starts with local audio, where the desktop "connecting"
// indicator then stayed on screen for the full 20s safety timeout while the
// music played.
//
// An observed property is pushed at its current value the moment
// observe_property is issued, so it survives a late subscription. That is what
// must end the attempt.
func TestCoreIdleFalseEndsStartingAttemptWithoutPlaybackRestart(t *testing.T) {
	p := newStartedPlayer()
	p.handleEvent([]byte(`{"event":"property-change","name":"core-idle","data":false}`))
	if startingFlag(p) {
		t.Fatal("starting still set after core-idle went false; " +
			"a cold start that misses playback-restart would strand the desktop indicator")
	}
}

// core-idle true is mpv reporting that it is NOT presenting — buffering, idle,
// or paused. Clearing on it would end the indicator precisely while the thing
// it exists to explain is still happening.
func TestCoreIdleTrueLeavesStartingAttemptArmed(t *testing.T) {
	p := newStartedPlayer()
	p.handleEvent([]byte(`{"event":"property-change","name":"core-idle","data":true}`))
	if !startingFlag(p) {
		t.Fatal("core-idle true cleared starting; it means playback is not running")
	}
}

// A null property value is mpv saying "unavailable", not "playing".
func TestCoreIdleUnavailableLeavesStartingAttemptArmed(t *testing.T) {
	p := newStartedPlayer()
	p.handleEvent([]byte(`{"event":"property-change","name":"core-idle","data":null}`))
	if !startingFlag(p) {
		t.Fatal("a null core-idle cleared starting")
	}
}
