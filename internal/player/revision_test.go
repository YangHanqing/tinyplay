package player

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWaitPlaybackRevisionReturnsImmediatelyWhenMismatched(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.playbackRevision = 5
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	p.WaitPlaybackRevision(ctx, 4)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("mismatched revision should return immediately, took %v", elapsed)
	}
}

func TestWaitPlaybackRevisionBlocksUntilBump(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.playbackRevision = 3
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	started := make(chan struct{})
	go func() {
		defer wg.Done()
		close(started)
		p.WaitPlaybackRevision(ctx, 3)
	}()
	<-started
	// Give the waiter time to park on the channel.
	time.Sleep(30 * time.Millisecond)

	p.mu.Lock()
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waiter did not wake after revision bump")
	}
	if got := p.State()["playback_revision"]; got != uint64(4) {
		t.Fatalf("playback_revision = %#v, want 4", got)
	}
}

func TestWaitPlaybackRevisionRespectsContextCancel(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.playbackRevision = 1
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.WaitPlaybackRevision(ctx, 1)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waiter did not return on context cancel")
	}
}

func TestBumpPlaybackRevisionWakesMultipleWaiters(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.playbackRevision = 10
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	const n = 4
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			p.WaitPlaybackRevision(ctx, 10)
		}()
	}
	time.Sleep(30 * time.Millisecond)

	p.mu.Lock()
	p.bumpPlaybackRevisionLocked()
	// Second bump must not panic (fresh channel, not double-close).
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not all waiters woke after revision bumps")
	}
}

func TestClearCompletedPlaybackNotifiesWaiters(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.ctx = PlayContext{ItemID: "e1", SeriesID: "s", PlaybackCompleted: true}
	p.playbackRevision = 2
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.WaitPlaybackRevision(ctx, 2)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	p.ClearCompletedPlayback()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ClearCompletedPlayback should wake revision waiters")
	}
}
