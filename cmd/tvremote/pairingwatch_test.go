package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// pairingStatusStub serves whatever /desktop/pairing body the test currently
// wants, so the watcher can be driven through a request's whole life.
type pairingStatusStub struct {
	mu       sync.Mutex
	body     string
	status   int
	requests int
}

func (s *pairingStatusStub) set(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
}

func (s *pairingStatusStub) setStatus(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
}

func (s *pairingStatusStub) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *pairingStatusStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	body, status := s.body, s.status
	s.requests++
	s.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// raiseRecorder counts window raises without blocking the watcher.
type raiseRecorder struct {
	mu    sync.Mutex
	count int
}

func (r *raiseRecorder) raise() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
}

func (r *raiseRecorder) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// waitFor polls cond until it holds or the deadline passes, so the assertions do
// not depend on how many ticks happened to land.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func startWatcher(t *testing.T, stub *pairingStatusStub, raiser *raiseRecorder) {
	t.Helper()
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)

	// The production interval is two seconds; the loop is identical, so drive it
	// fast rather than making the test wait real time.
	original := pairingPollIntervalForTest
	pairingPollIntervalForTest = 5 * time.Millisecond
	t.Cleanup(func() { pairingPollIntervalForTest = original })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go watchPairingRequests(ctx, server.URL, raiser.raise)
}

func TestPairingWatchRaisesOncePerRequest(t *testing.T) {
	stub := &pairingStatusStub{body: `{"locked":false,"device_count":0}`}
	raiser := &raiseRecorder{}
	startWatcher(t, stub, raiser)

	waitFor(t, "the watcher to start polling", func() bool { return stub.hits() > 2 })
	if got := raiser.total(); got != 0 {
		t.Fatalf("no pending request must not raise the window, got %d raises", got)
	}

	stub.set(`{"pending":{"nonce":"abc","code":"1234"},"device_count":0}`)
	waitFor(t, "the window to be raised", func() bool { return raiser.total() >= 1 })

	// The card stays on screen for the whole request; the window must not keep
	// clawing back to the front while it does.
	before := raiser.total()
	waitFor(t, "several more polls", func() bool { return stub.hits() > 0 })
	time.Sleep(60 * time.Millisecond)
	if got := raiser.total(); got != before {
		t.Fatalf("a still-pending request re-raised the window: %d -> %d", before, got)
	}
}

func TestPairingWatchRaisesAgainForTheNextRequest(t *testing.T) {
	stub := &pairingStatusStub{body: `{"pending":{"nonce":"first","code":"1111"}}`}
	raiser := &raiseRecorder{}
	startWatcher(t, stub, raiser)

	waitFor(t, "the first raise", func() bool { return raiser.total() >= 1 })

	// Answered or expired.
	stub.set(`{"device_count":1}`)
	time.Sleep(40 * time.Millisecond)

	stub.set(`{"pending":{"nonce":"second","code":"2222"}}`)
	waitFor(t, "the second raise", func() bool { return raiser.total() >= 2 })
}

func TestPairingWatchIgnoresUnavailableCore(t *testing.T) {
	// The watcher can start before the core is listening, and /desktop/ is
	// loopback-guarded; neither may be mistaken for a pending request.
	stub := &pairingStatusStub{body: `{"detail":"loopback only"}`}
	stub.setStatus(http.StatusForbidden)
	raiser := &raiseRecorder{}
	startWatcher(t, stub, raiser)

	waitFor(t, "several failed polls", func() bool { return stub.hits() > 3 })
	if got := raiser.total(); got != 0 {
		t.Fatalf("a non-200 response raised the window %d times", got)
	}

	// It must recover once the core answers properly rather than giving up.
	stub.setStatus(http.StatusOK)
	stub.set(`{"pending":{"nonce":"later","code":"3333"}}`)
	waitFor(t, "the watcher to recover", func() bool { return raiser.total() >= 1 })
}

func TestPairingWatchIgnoresGarbageBody(t *testing.T) {
	stub := &pairingStatusStub{body: `<html>not json</html>`}
	raiser := &raiseRecorder{}
	startWatcher(t, stub, raiser)

	waitFor(t, "several polls", func() bool { return stub.hits() > 3 })
	if got := raiser.total(); got != 0 {
		t.Fatalf("an unparseable body raised the window %d times", got)
	}
}

func TestPairingWatchStopsWithContext(t *testing.T) {
	stub := &pairingStatusStub{body: `{"device_count":0}`}
	server := httptest.NewServer(stub)
	defer server.Close()

	original := pairingPollIntervalForTest
	pairingPollIntervalForTest = 5 * time.Millisecond
	defer func() { pairingPollIntervalForTest = original }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchPairingRequests(ctx, server.URL, func() {})
		close(done)
	}()

	waitFor(t, "the watcher to poll", func() bool { return stub.hits() > 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not stop when its context was cancelled")
	}
}
