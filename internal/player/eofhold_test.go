package player

import (
	"bufio"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The whole point of the post-EOF hold: mpv must outlive the file it just
// finished, and must keep a window while it does. Without either flag an
// autoplay transition tears the window down and cold-starts a new process,
// which the viewer sees as the player closing and reopening.
func TestSpawnArgsKeepMPVAliveAndVisibleAcrossEOF(t *testing.T) {
	args := strings.Join(initialMPVArgs("http://example/video", "/tmp/test"), " ")
	for _, want := range []string{"--idle=yes", "--force-window=yes", "--keep-open=no"} {
		if !strings.Contains(args, want) {
			t.Fatalf("spawn args = %q, want %s", args, want)
		}
	}
}

func eofPlayer(t *testing.T, watched time.Duration) *Player {
	t.Helper()
	p := New()
	p.mu.Lock()
	p.running = true
	p.ctx = PlayContext{ServerID: "srv", ItemID: "e1", SeriesID: "s1", Title: "E1"}
	p.mu.Unlock()
	atomic.StoreInt64(&p.startPosTicks, 0)
	atomic.StoreInt64(&p.lastPosTicks, int64(watched/100))
	return p
}

// A finished episode that can chain leaves mpv up, so the next title is a
// loadfile into the window already on screen.
func TestNaturalEOFHoldsTheProcessForTheHostTransition(t *testing.T) {
	p := eofPlayer(t, 30*time.Minute)
	reported := make(chan PlayContext, 1)
	p.NaturalEOFReporter = func(ctx PlayContext, revision uint64) { reported <- ctx }

	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))

	select {
	case ctx := <-reported:
		if !ctx.PlaybackCompleted || ctx.ItemID != "e1" {
			t.Fatalf("reported context = %+v", ctx)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("natural EOF never reached the host autoplay hand-off")
	}
	p.mu.Lock()
	holding := p.idleHolding
	p.mu.Unlock()
	if !holding {
		t.Fatal("mpv was not held open; the transition would have to cold-start a new process")
	}
}

// The host deciding there is no next title is what closes the window. Leaving
// the hold in place would park a black fullscreen window on the TV for good.
func TestClearCompletedPlaybackEndsTheHold(t *testing.T) {
	p := eofPlayer(t, 30*time.Minute)
	p.NaturalEOFReporter = func(PlayContext, uint64) {}
	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))

	p.ClearCompletedPlayback()

	p.mu.Lock()
	holding := p.idleHolding
	item := p.ctx.ItemID
	p.mu.Unlock()
	if holding {
		t.Fatal("hold survived the host saying there is no next title")
	}
	if item != "" {
		t.Fatalf("item_id = %q, want the completed context cleared", item)
	}
}

// A title nothing can chain off (here: it barely advanced, so the EOF is a
// degenerate open rather than a viewing) must not hold the process at all.
func TestNaturalEOFWithNothingToChainDoesNotHold(t *testing.T) {
	p := eofPlayer(t, time.Second)
	p.NaturalEOFReporter = func(PlayContext, uint64) {
		t.Error("a title that never really played was handed to autoplay")
	}

	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))

	p.mu.Lock()
	holding := p.idleHolding
	item := p.ctx.ItemID
	p.mu.Unlock()
	if holding {
		t.Fatal("mpv was held open with no transition to wait for")
	}
	if item != "" {
		t.Fatalf("item_id = %q, want the context cleared", item)
	}
}

// loadfile ... replace ends the outgoing title with reason "stop". Treating
// that as the end of the session would fire a stop report and clear the
// context of the title that is at that moment loading.
func TestReplacedTitleIsNotAnEndOfPlayback(t *testing.T) {
	p := eofPlayer(t, 30*time.Minute)
	p.NaturalEOFReporter = func(PlayContext, uint64) { t.Error("loadfile replace reached the EOF hand-off") }

	p.handleEvent([]byte(`{"event":"end-file","reason":"stop"}` + "\n"))

	p.mu.Lock()
	handled := p.eofHandled
	item := p.ctx.ItemID
	p.mu.Unlock()
	if handled || item != "e1" {
		t.Fatalf("eofHandled = %v, item_id = %q; the replacing title's context was disturbed", handled, item)
	}
}

// The hand-off is once per playback: the process waiter must not repeat it
// when the held-open process finally exits, or the finished title is
// stop-reported twice.
func TestNaturalEOFIsHandedOffOnlyOnce(t *testing.T) {
	p := eofPlayer(t, 30*time.Minute)
	var calls int32
	p.NaturalEOFReporter = func(PlayContext, uint64) { atomic.AddInt32(&calls, 1) }

	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))
	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("hand-off ran %d times, want 1", got)
	}
}

// attachIPCRecorder gives an existing player a live IPC connection and records
// everything it writes, so a test can tell "asked mpv to quit" from "left it
// alone". recordingPlayer (loopfile_test.go) is the same trick for a player a
// test constructs itself.
func attachIPCRecorder(t *testing.T, p *Player) *ipcRecorder {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	rec := &ipcRecorder{}
	go func() {
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			rec.mu.Lock()
			rec.lines = append(rec.lines, scanner.Text())
			rec.mu.Unlock()
		}
	}()
	p.setConn(client)
	return rec
}

// waitContains polls the recorder: net.Pipe hands a write over synchronously,
// but the scanner goroutine appends the line just after the write returns.
func waitContains(rec *ipcRecorder, fragment string) bool {
	for i := 0; i < 40; i++ {
		if rec.contains(fragment) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// The event reader is a single goroutine and the EOF hand-off blocks it for a
// network stop report, so the previous file's "eof" can still be pending when
// a new play has already loadfile'd into the same process. Applying it to the
// incoming title would stop-report it at its resume seed and close its window
// — the same process both titles now share.
func TestStaleEOFDoesNotEndTheTitleThatReplacedIt(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	p := eofPlayer(t, 30*time.Minute)
	rec := attachIPCRecorder(t, p)
	p.NaturalEOFReporter = func(PlayContext, uint64) {
		t.Error("a stale EOF was chained off as if the new title had finished")
	}

	p.Play("http://example/e2", PlayOptions{ServerID: "srv", ItemID: "e2", Title: "E2"})
	p.handleEvent([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))

	p.mu.Lock()
	item, handled, holding := p.ctx.ItemID, p.eofHandled, p.idleHolding
	p.mu.Unlock()
	if item != "e2" || handled || holding {
		t.Fatalf("item_id = %q, eofHandled = %v, holding = %v; the incoming title was treated as finished", item, handled, holding)
	}
	if waitContains(rec, `"quit"`) {
		t.Fatal("the stale EOF quit the process the new title had already been loaded into")
	}
}

// The other half of the same hazard: the EOF really is this title's, but the
// bookkeeping runs long enough for a manual play to claim the held-open
// process before the teardown closes it.
func TestPostEOFQuitSkipsAProcessAnotherPlayClaimed(t *testing.T) {
	p := eofPlayer(t, time.Second)
	rec := attachIPCRecorder(t, p)
	p.mu.Lock()
	revision, proc := p.playbackRevision, p.proc
	// What Play does under claimMu once it has loaded the next item.
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()

	p.quitProcessIfUnclaimed(revision, proc)

	if waitContains(rec, `"quit"`) {
		t.Fatal("mpv was closed out from under the playback that had claimed it")
	}
}

// ...and it must still close the window when nothing claimed it, or a finished
// file leaves a black fullscreen mpv on the TV for good.
func TestPostEOFQuitClosesAnUnclaimedProcess(t *testing.T) {
	p := eofPlayer(t, time.Second)
	rec := attachIPCRecorder(t, p)
	p.mu.Lock()
	revision, proc := p.playbackRevision, p.proc
	p.mu.Unlock()

	p.quitProcessIfUnclaimed(revision, proc)

	if !waitContains(rec, `"quit"`) {
		t.Fatal("the process was left running with nothing to play")
	}
}
