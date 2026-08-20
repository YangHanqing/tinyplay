package player

import (
	"testing"
	"time"
)

// These tests model the player lifecycle at the synchronization points that
// matter to the shelf. mpv itself is deliberately not involved: the invariant
// is that a report return, a context clear, and process exit can arrive in any
// order.
func TestResumeRefreshGenerationWaitsForReportAndIdle(t *testing.T) {
	t.Run("natural end", func(t *testing.T) {
		p := resumeRefreshTestPlayer("movie-a")
		release := queueBlockingStopReport(t, p)

		// mpv has stopped, but EOF/autoplay bookkeeping still owns the title.
		setResumeRefreshState(p, false, "movie-a")
		setResumeRefreshState(p, false, "")
		release()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 1)
	})

	t.Run("explicit stop", func(t *testing.T) {
		p := resumeRefreshTestPlayer("movie-a")
		release := queueBlockingStopReport(t, p)

		// Stop clears the context before quit has necessarily reaped mpv.
		setResumeRefreshState(p, true, "")
		release()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 0)

		setResumeRefreshState(p, false, "")
		assertResumeRefreshGeneration(t, p, 1)
	})

	t.Run("mid-title switch", func(t *testing.T) {
		p := resumeRefreshTestPlayer("movie-a")
		releaseA := queueBlockingStopReport(t, p)

		// The outgoing report may return after B has already claimed playback.
		setResumeRefreshState(p, true, "movie-b")
		releaseA()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 0)

		releaseB := queueBlockingStopReport(t, p)
		setResumeRefreshState(p, false, "")
		releaseB()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 1)
	})

	t.Run("back-to-back sessions", func(t *testing.T) {
		p := resumeRefreshTestPlayer("movie-a")
		releaseA := queueBlockingStopReport(t, p)

		// A ends, then B starts before A's media-server write returns.
		setResumeRefreshState(p, false, "")
		setResumeRefreshState(p, true, "movie-b")
		releaseA()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 0)

		releaseB := queueBlockingStopReport(t, p)
		setResumeRefreshState(p, false, "")
		releaseB()
		waitForStopReports(t, p, 0)
		assertResumeRefreshGeneration(t, p, 1)
	})
}

func resumeRefreshTestPlayer(itemID string) *Player {
	return &Player{
		running:   true,
		ctx:       PlayContext{ItemID: itemID},
		liveProps: map[string]any{},
	}
}

func queueBlockingStopReport(t *testing.T, p *Player) func() {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	p.StopReporter = func(serverID, itemID, sessionID string, posTicks int64, durationSeconds float64, mediaSourceID string) {
		close(started)
		<-release
	}
	p.fireStopReport()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stop reporter did not start")
	}
	return func() { close(release) }
}

func setResumeRefreshState(p *Player, running bool, itemID string) {
	p.mu.Lock()
	p.running = running
	p.ctx = PlayContext{ItemID: itemID}
	p.flushResumeRefreshLocked()
	p.mu.Unlock()
}

func waitForStopReports(t *testing.T, p *Player, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := p.stopReportsInFlight
		p.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	got := p.stopReportsInFlight
	p.mu.Unlock()
	t.Fatalf("stop reports in flight = %d, want %d", got, want)
}

func assertResumeRefreshGeneration(t *testing.T, p *Player, want uint64) {
	t.Helper()
	p.mu.Lock()
	got := p.resumeRefreshGeneration
	p.mu.Unlock()
	if got != want {
		t.Fatalf("resume refresh generation = %d, want %d", got, want)
	}
}
