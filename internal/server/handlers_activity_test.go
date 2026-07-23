package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/player"
	"tvremote/internal/website"
)

func activityTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	previousBroker := websiteBroker
	t.Cleanup(func() { websiteBroker = previousBroker })
	websiteBroker = website.NewBroker(func() {})
	s := &Server{player: player.New(), webFS: nil, latestSwitch: map[string]int{}, iptvRecoveries: map[string]*iptvRecovery{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/activity/state", s.activityState)
	return s, withGuard(mux)
}

func getActivity(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/activity/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	return out
}

func TestActivityIdleWhenNothingActive(t *testing.T) {
	_, h := activityTestServer(t)
	out := getActivity(t, h)
	if out["surface"] != "idle" {
		t.Fatalf("expected idle, got %+v", out)
	}
	if out["phase"] != "idle" {
		t.Fatalf("expected idle phase, got %+v", out)
	}
}

func TestActivitySurfaceIsWebsiteWhenWindowOpen(t *testing.T) {
	_, h := activityTestServer(t)
	if _, err := websiteBroker.RequestOpen(website.SiteBilibili); err != nil {
		t.Fatal(err)
	}
	out := getActivity(t, h)
	if out["surface"] != "website" {
		t.Fatalf("expected website surface, got %+v", out)
	}
	if out["phase"] != "opening" || out["engine"] != "webview" || out["source_type"] != "website" {
		t.Fatalf("opening website identity=%+v", out)
	}
	// The URL boundary must hold: no full document URL is ever exposed here,
	// only the allowlisted site id (empty until a navigation report arrives).
	if strings.Contains(getBody(t, h), "current_url") || strings.Contains(getBody(t, h), "http") {
		t.Fatalf("activity must not leak a website URL")
	}
	web, _ := out["website"].(map[string]any)
	if web == nil || web["kind"] != "website" {
		t.Fatalf("website payload=%+v", out["website"])
	}

	open := true
	websiteBroker.ApplyReport(website.Report{
		Open:       &open,
		Status:     "navigated",
		Action:     website.ActionOpen,
		CurrentURL: "https://www.bilibili.com/video/BV1?token=secret",
	})
	out = getActivity(t, h)
	if out["phase"] != "active" {
		t.Fatalf("reported website phase=%+v", out)
	}
	web, _ = out["website"].(map[string]any)
	if web == nil || web["site_id"] != website.SiteBilibili {
		t.Fatalf("reported website payload=%+v", out["website"])
	}
	if body := getBody(t, h); strings.Contains(body, "token=secret") || strings.Contains(body, "current_url") {
		t.Fatalf("activity leaked website URL: %s", body)
	}
}

func getBody(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/activity/state", nil))
	return rec.Body.String()
}

func TestActivityPlaybackPresentDerivation(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  bool
	}{
		{"running", map[string]any{"running": true}, true},
		{"item context", map[string]any{"running": false, "item_id": "x"}, true},
		{"live channel", map[string]any{"running": false, "channel_id": "c"}, true},
		{"empty", map[string]any{"running": false}, false},
		{
			"finished history (no autoplay) is idle",
			map[string]any{"running": false, "playback_completed": true, "item_id": "x"},
			false,
		},
		{
			"finished but autoplay coordinating is playback",
			map[string]any{"running": false, "playback_completed": true, "autoplay_status": autoplayStatusNextAvailable},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := activityPlaybackPresent(tc.state); got != tc.want {
				t.Fatalf("activityPlaybackPresent(%+v)=%v want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestActivityPlaybackPhase(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  string
	}{
		{"running", map[string]any{"running": true}, activityPhaseActive},
		{"context before running", map[string]any{"item_id": "x"}, activityPhaseStarting},
		{"finding next", map[string]any{"autoplay_status": autoplayStatusFindingNext}, activityPhaseTransition},
		{"next available", map[string]any{"autoplay_status": autoplayStatusNextAvailable}, activityPhaseTransition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := activityPlaybackPhase(tc.state); got != tc.want {
				t.Fatalf("activityPlaybackPhase(%+v)=%q want %q", tc.state, got, tc.want)
			}
		})
	}
}
