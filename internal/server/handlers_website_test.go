package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/player"
	"tvremote/internal/website"
)

func websiteTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	// Isolate global broker for this test package run.
	websiteBroker = website.NewBroker(func() {})
	s := &Server{player: player.New(), webFS: nil, latestSwitch: map[string]int{}, iptvRecoveries: map[string]*iptvRecovery{}}
	// Minimal router with only website routes + guard.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/website/state", s.websiteState)
	mux.HandleFunc("POST /api/website/open", s.websiteOpen)
	mux.HandleFunc("POST /api/website/close", s.websiteClose)
	mux.HandleFunc("POST /api/website/action", s.websiteAction)
	mux.HandleFunc("GET /desktop/website/poll", s.websiteShellPoll)
	mux.HandleFunc("POST /desktop/website/report", s.websiteShellReport)
	mux.HandleFunc("GET /desktop/website/controller.js", s.websiteControllerJS)
	return s, withGuard(mux)
}

func TestWebsiteStateFreshEmpty(t *testing.T) {
	_, h := websiteTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/website/state", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap website.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.CurrentSiteID != "" || snap.DesiredOpen || snap.ReportedOpen {
		t.Fatalf("fresh snapshot=%+v", snap)
	}
	if len(snap.Catalog) != 5 || snap.Catalog[0].ID != website.SiteBilibili || snap.Catalog[4].ID != website.SiteDouyin {
		t.Fatalf("catalog=%+v", snap.Catalog)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["mode"]; ok {
		t.Fatalf("snapshot must not include mode: %v", raw)
	}
	if _, ok := raw["selected_site_id"]; ok {
		t.Fatalf("snapshot must not include selected_site_id: %v", raw)
	}
	if _, ok := raw["current_url"]; ok {
		t.Fatalf("snapshot must not include current_url: %v", raw)
	}
}

func TestWebsiteModeAndSiteSelectionEndpointsRemoved(t *testing.T) {
	_, h := websiteTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPut, "/api/website/mode", `{"mode":"website"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("mode endpoint must not exist; status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPut, "/api/website/site", `{"site_id":"bilibili"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("site selection endpoint must not exist; status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebsiteRejectUnknownSiteAndAction(t *testing.T) {
	_, h := websiteTestServer(t)
	rec := httptest.NewRecorder()
	req := jsonReq(http.MethodPost, "/api/website/open", `{"site_id":"https://evil.example"}`)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = jsonReq(http.MethodPost, "/api/website/open", `{}`)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty site_id status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = jsonReq(http.MethodPost, "/api/website/action", `{"action":"eval","text":"1+1"}`)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebsiteOpenQueuesAllowlistedURL(t *testing.T) {
	_, h := websiteTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPost, "/api/website/open", `{"site_id":"youku"}`))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap website.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.CurrentSiteID != "" {
		t.Fatalf("open must not set current from request: %+v", snap)
	}
	cmd, ok := websiteBroker.PendingAfter(0)
	if !ok || cmd.Action != website.ActionOpen {
		t.Fatalf("cmd=%+v ok=%v", cmd, ok)
	}
	site, _ := website.SiteByID(website.SiteYouku)
	if cmd.URL != site.URL || cmd.SiteID != site.ID {
		t.Fatalf("url=%s site=%s", cmd.URL, cmd.SiteID)
	}
}

func TestWebsiteReportDerivesCurrentSiteWithoutLeakingURL(t *testing.T) {
	_, h := websiteTestServer(t)
	websiteBroker.RequestOpen(website.SiteBilibili)

	rec := httptest.NewRecorder()
	req := jsonReq(http.MethodPost, "/desktop/website/report",
		`{"open":true,"status":"navigated","action":"navigation","current_url":"https://www.bilibili.com/video/1?token=secret"}`)
	req.RemoteAddr = "127.0.0.1:9"
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap website.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.CurrentSiteID != website.SiteBilibili || !snap.ReportedOpen {
		t.Fatalf("snap=%+v", snap)
	}
	if strings.Contains(rec.Body.String(), "token=secret") || strings.Contains(rec.Body.String(), "current_url") {
		t.Fatalf("response must not echo full URL: %s", rec.Body.String())
	}

	// Shell capability IDs are filtered through the current site's fixed
	// profile before the phone sees or can invoke them.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPost, "/api/website/action", `{"action":"capabilities"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("capability probe status=%d body=%s", rec.Code, rec.Body.String())
	}
	probe, ok := websiteBroker.PendingAfter(1)
	if !ok || probe.Action != website.ActionCapabilities {
		t.Fatalf("capability command missing: %+v", probe)
	}
	rec = httptest.NewRecorder()
	req = jsonReq(http.MethodPost, "/desktop/website/report",
		fmt.Sprintf(`{"open":true,"status":"capabilities","action":"capabilities","command_id":%d,"more_actions":["evil","danmaku_toggle"]}`, probe.ID))
	req.RemoteAddr = "127.0.0.1:9"
	h.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.MoreActions) != 1 || snap.MoreActions[0].ID != website.ActionDanmakuToggle {
		t.Fatalf("filtered More actions=%+v", snap.MoreActions)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPost, "/api/website/action", `{"action":"danmaku_toggle"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("approved More action status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Cross-site navigation.
	rec = httptest.NewRecorder()
	req = jsonReq(http.MethodPost, "/desktop/website/report",
		`{"open":true,"current_url":"https://www.iqiyi.com/"}`)
	req.RemoteAddr = "127.0.0.1:9"
	h.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &snap)
	if snap.CurrentSiteID != website.SiteIQIYI {
		t.Fatalf("expected iqiyi, got %+v", snap)
	}
}

func TestWebsiteShellEndpointsRejectNonLoopback(t *testing.T) {
	_, h := websiteTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/desktop/website/poll", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("poll status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = jsonReq(http.MethodPost, "/desktop/website/report", `{"status":"ok"}`)
	req.RemoteAddr = "10.0.0.2:9"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("report status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/desktop/website/controller.js", nil)
	req.RemoteAddr = "8.8.8.8:53"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("controller status=%d", rec.Code)
	}
}

func TestWebsiteShellLoopbackOK(t *testing.T) {
	_, h := websiteTestServer(t)
	// Seed a command.
	if _, err := websiteBroker.RequestOpen(website.SiteBilibili); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/desktop/website/poll?after=0", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"action":"open"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/desktop/website/controller.js", nil)
	req.RemoteAddr = "[::1]:1"
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "__tinyplayWebsite") {
		t.Fatalf("controller status=%d", rec.Code)
	}
}

func TestRequestWebsiteCloseMutualExclusion(t *testing.T) {
	websiteBroker = website.NewBroker(func() {})
	if _, err := websiteBroker.RequestOpen(website.SiteBilibili); err != nil {
		t.Fatal(err)
	}
	if !websiteBroker.Snapshot().DesiredOpen {
		t.Fatal("expected open")
	}
	RequestWebsiteClose()
	if websiteBroker.Snapshot().DesiredOpen {
		t.Fatal("play path should request website close")
	}
}

func TestIsLoopbackRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	if !isLoopbackRequest(req) {
		t.Fatal("127 should be loopback")
	}
	req.RemoteAddr = "192.168.0.1:1"
	if isLoopbackRequest(req) {
		t.Fatal("LAN should not be loopback")
	}
}
