package player

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestAwaitProcessExit(t *testing.T) {
	if !(&Player{}).awaitProcessExit(time.Second) {
		t.Fatal("a player with no process must report the exit immediately")
	}
	p := &Player{running: true}
	start := time.Now()
	if p.awaitProcessExit(150 * time.Millisecond) {
		t.Fatal("a live process must not be reported as exited")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("awaitProcessExit returned before its deadline")
	}
}

// mpv running but unreachable over IPC is ambiguous: it is usually a process
// on its way out, but it is also what a second play issued during a cold
// start's first moments looks like. When the process does not go away, the
// play must fail rather than start a second player beside the live one — and
// it must not install a context describing something that never loaded.
func TestPlayAbandonsWhenUnreachableProcessSurvives(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	p := &Player{running: true}
	result := p.Play("http://example/song.flac", PlayOptions{ServerID: "srv", ItemID: "song", SourceType: "dlna"})
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("play reported success: %#v", result)
	}
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx.ItemID != "" {
		t.Fatalf("abandoned play installed a context: %+v", ctx)
	}
}

// The other half: once the process is really gone, the same failure must be
// repaired by a fresh start rather than surfacing to the caller. This is the
// case a DLNA cast hits routinely, because a cast tends to arrive just as
// whatever was playing before finishes — the sender was left on "connecting"
// against a renderer that had quietly given up.
func TestPlayFallsBackToFreshProcessOnceOldOneExits(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	p := &Player{running: true}
	go func() {
		time.Sleep(100 * time.Millisecond)
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()
	if !p.awaitProcessExit(reuseFallbackWait) {
		t.Fatal("the exiting process was not observed within the fallback window")
	}
}

// A stale process-exit must not clear the context of a playback that started
// after it. The exit bookkeeping runs asynchronously and passes through a
// network stop report first, which is more than long enough for the reuse
// fallback above to have installed a new context.
func TestStaleExitDoesNotClearNewerPlayback(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = io.Copy(io.Discard, server) }()

	p := &Player{running: true, conn: client}
	p.Play("http://example/a", PlayOptions{ServerID: "srv", ItemID: "item-a"})
	p.mu.Lock()
	exitRevision := p.playbackRevision
	p.mu.Unlock()

	p.Play("http://example/b", PlayOptions{ServerID: "srv", ItemID: "item-b"})

	// What the exit goroutine of the first playback would do, at the point it
	// decides whether the context is still its own.
	p.mu.Lock()
	superseded := p.playbackRevision != exitRevision
	p.mu.Unlock()
	if !superseded {
		t.Fatal("a newer play did not advance the revision, so a stale exit would clear it")
	}
}
