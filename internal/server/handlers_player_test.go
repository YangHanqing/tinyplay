package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"tvremote/internal/player"
)

func TestPlayerStateImmediateWithoutAfterRevision(t *testing.T) {
	p := player.New()
	h := New(p).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/player/state", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("immediate state took %v, want fast response", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["running"]; !ok {
		t.Fatal("response missing running")
	}
	if _, ok := body["playback_revision"]; !ok {
		t.Fatal("response missing playback_revision")
	}
}

func TestPlayerStateLongPollMismatchedRevisionReturnsImmediately(t *testing.T) {
	p := player.New()
	// Bump once so revision is 1.
	p.Stop()
	rev, _ := p.State()["playback_revision"].(uint64)
	if rev == 0 {
		t.Fatal("expected non-zero revision after Stop")
	}

	h := New(p).Handler()
	// Client is behind: after_revision is older than current.
	req := httptest.NewRequest(http.MethodGet, "/api/player/state?after_revision=0", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("mismatched revision should not wait, took %v", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPlayerStateLongPollWaitsUntilRevisionChanges(t *testing.T) {
	p := player.New()
	h := New(p).Handler()
	rev, _ := p.State()["playback_revision"].(uint64)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet,
		"/api/player/state?after_revision="+strconv.FormatUint(rev, 10), nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	var wg sync.WaitGroup
	wg.Add(1)
	started := make(chan struct{})
	go func() {
		defer wg.Done()
		close(started)
		h.ServeHTTP(rec, req)
	}()
	<-started
	time.Sleep(40 * time.Millisecond)

	// Trigger a revision bump via Stop (works without mpv).
	p.Stop()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("long-poll did not complete after revision bump")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := uint64FromJSON(body["playback_revision"])
	if got <= rev {
		t.Fatalf("playback_revision = %d, want > %d", got, rev)
	}
}

func TestPlayerStateLongPollRespectsRequestCancel(t *testing.T) {
	p := player.New()
	h := New(p).Handler()
	rev, _ := p.State()["playback_revision"].(uint64)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet,
		"/api/player/state?after_revision="+strconv.FormatUint(rev, 10), nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("long-poll did not return on request cancel")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func uint64FromJSON(v any) uint64 {
	switch n := v.(type) {
	case float64:
		return uint64(n)
	case json.Number:
		u, _ := n.Int64()
		return uint64(u)
	case uint64:
		return n
	case int:
		return uint64(n)
	default:
		return 0
	}
}
