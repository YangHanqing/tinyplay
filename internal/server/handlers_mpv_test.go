package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

func mpvTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := &Server{player: player.New(), webFS: nil, latestSwitch: map[string]int{}, iptvRecoveries: map[string]*iptvRecovery{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /desktop/mpv", s.mpvStatus)
	mux.HandleFunc("POST /desktop/mpv", s.mpvSetCustom)
	return s, withGuard(mux)
}

// writeFakeMPVScript drops an executable script that answers `--version` like
// mpv would. Skipped on Windows, matching internal/player's own fake-mpv test
// helper (ValidateMPV shells out to the path directly).
func writeFakeMPVScript(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake mpv script requires a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\necho 'mpv 0.37.0'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func loopbackRequest(method, target string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.RemoteAddr = "127.0.0.1:9999"
	return req
}

func TestMpvStatusIsLoopbackOnly(t *testing.T) {
	_, h := mpvTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/desktop/mpv", nil)
	req.RemoteAddr = "192.168.1.50:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-loopback caller, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMpvStatusReportsDefaultState(t *testing.T) {
	_, h := mpvTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/desktop/mpv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["custom_configured"] != false {
		t.Fatalf("expected custom_configured=false by default: %v", out)
	}
}

func TestMpvSetCustomRejectsInvalidPath(t *testing.T) {
	_, h := mpvTestServer(t)
	body, _ := json.Marshal(map[string]string{"path": "/no/such/mpv/binary"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodPost, "/desktop/mpv", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid path, got %d: %s", rec.Code, rec.Body.String())
	}
	if config.MpvExe() != "" {
		t.Fatalf("an invalid path must not be persisted, got %q", config.MpvExe())
	}
}

func TestMpvSetCustomPersistsValidPath(t *testing.T) {
	_, h := mpvTestServer(t)
	fake := writeFakeMPVScript(t, "fake-mpv")
	body, _ := json.Marshal(map[string]string{"path": fake})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodPost, "/desktop/mpv", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if config.MpvExe() != fake {
		t.Fatalf("expected persisted path %q, got %q", fake, config.MpvExe())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["source"] != "custom" || out["custom_valid"] != true {
		t.Fatalf("unexpected response: %v", out)
	}

	// Clearing goes back to "no custom path configured".
	body, _ = json.Marshal(map[string]string{"path": ""})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodPost, "/desktop/mpv", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	if config.MpvExe() != "" {
		t.Fatalf("expected cleared custom path, got %q", config.MpvExe())
	}
}

func TestMpvSetCustomIsLoopbackOnly(t *testing.T) {
	_, h := mpvTestServer(t)
	body, _ := json.Marshal(map[string]string{"path": ""})
	req := httptest.NewRequest(http.MethodPost, "/desktop/mpv", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.50:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-loopback caller, got %d: %s", rec.Code, rec.Body.String())
	}
}
