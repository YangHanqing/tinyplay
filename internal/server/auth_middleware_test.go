package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/auth"
	"tvremote/internal/player"
)

// lanRequest is how an unpaired phone (or an attacker) on the network arrives:
// httptest's default RemoteAddr is already a non-loopback address.
func lanRequest(method, path, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "192.168.1.77:51000"
	return req
}

func TestPairingBoundaryBlocksUnpairedLANDevices(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()

	// The two findings that motivated pairing: reading any file off the host,
	// and injecting keystrokes into the desktop.
	blocked := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/files/list?source_type=local&path=", ""},
		{http.MethodGet, "/api/files/stream?source_type=local&path=etc/passwd", ""},
		{http.MethodPost, "/api/system/input/action", `{"action":"type","text":"whoami"}`},
		{http.MethodGet, "/api/settings", ""},
		{http.MethodGet, "/api/player/state", ""},
		{http.MethodPost, "/api/player/command", `{"command":["quit"]}`},
		{http.MethodGet, "/api/servers", ""},
		{http.MethodPost, "/api/website/open", `{"site_id":"bilibili"}`},
	}
	for _, c := range blocked {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, lanRequest(c.method, c.path, c.body))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
		if rec.Header().Get("X-TinyPlay-Pairing") != "required" {
			t.Errorf("%s %s: missing pairing hint header", c.method, c.path)
		}
	}
}

func TestPairingBoundaryAllowsAPairedDevice(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()

	token := pairViaQR(t, h)
	for _, header := range []string{"Authorization", "X-TinyPlay-Token"} {
		req := lanRequest(http.MethodGet, "/api/settings", "")
		if header == "Authorization" {
			req.Header.Set(header, "Bearer "+token)
		} else {
			req.Header.Set(header, token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (%s)", header, rec.Code, rec.Body.String())
		}
	}

	// Poster images are <img src> requests, which cannot carry a header.
	req := lanRequest(http.MethodGet, "/api/settings", "")
	req.AddCookie(&http.Cookie{Name: tokenCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie auth: status = %d, want 200", rec.Code)
	}

	req = lanRequest(http.MethodGet, "/api/settings", "")
	req.Header.Set("Authorization", "Bearer "+token+"tampered")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token: status = %d, want 401", rec.Code)
	}
}

// pairViaQR walks the real handshake: read the secret the QR code carries, then
// exchange it for a token.
func pairViaQR(t *testing.T, h http.Handler) string {
	t.Helper()
	auth.Default.Refresh()
	body := `{"secret":"` + auth.Default.Secret() + `"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodPost, "/api/pair/qr", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("pair: status = %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("pair response has no token: %s", rec.Body.String())
	}
	return out.Token
}

func TestWrongQRSecretIsRejected(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()
	auth.Default.Refresh()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodPost, "/api/pair/qr", `{"secret":"AAAAAAAAAAAAAAAA"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pair_bad_secret") {
		t.Fatalf("expected a pair_bad_secret code: %s", rec.Body.String())
	}
}

// The whole point of the consent path: a phone that typed the address by hand
// gets in only after someone at the computer approves the code it displayed.
func TestConsentPairingRequiresLoopbackApproval(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()
	auth.Default.RevokeAll()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodPost, "/api/pair/request", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("request: status = %d (%s)", rec.Code, rec.Body.String())
	}
	var pending struct {
		Nonce string `json:"nonce"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil || pending.Nonce == "" {
		t.Fatalf("bad request response: %s", rec.Body.String())
	}

	// A device on the network must not be able to approve itself, and must not
	// be able to read the pending request's details.
	for _, path := range []string{"/desktop/pairing/approve", "/desktop/pairing/refresh", "/desktop/pairing/unpair-all"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, lanRequest(http.MethodPost, path, `{"nonce":"`+pending.Nonce+`"}`))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s from LAN = %d, want 403", path, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodGet, "/desktop/pairing", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("pairing status from LAN = %d, want 403", rec.Code)
	}

	// Still pending, so claiming yields no token yet.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodPost, "/api/pair/claim", `{"nonce":"`+pending.Nonce+`"}`))
	if !strings.Contains(rec.Body.String(), `"state":"pending"`) || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("claim before approval leaked state: %s", rec.Body.String())
	}

	// Approve from the computer itself.
	approve := httptest.NewRequest(http.MethodPost, "/desktop/pairing/approve", strings.NewReader(`{"nonce":"`+pending.Nonce+`"}`))
	approve.Header.Set("Content-Type", "application/json")
	approve.RemoteAddr = "127.0.0.1:60000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, approve)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: status = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, lanRequest(http.MethodPost, "/api/pair/claim", `{"nonce":"`+pending.Nonce+`"}`))
	var claimed struct {
		State string `json:"state"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.State != "approved" || claimed.Token == "" {
		t.Fatalf("claim after approval = %+v", claimed)
	}

	req := lanRequest(http.MethodGet, "/api/settings", "")
	req.Header.Set("Authorization", "Bearer "+claimed.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approved device blocked: %d", rec.Code)
	}
}

// The QR image encodes the pairing secret, so it must never be readable from the
// network — otherwise the QR path would be worthless.
func TestQRImageAndLogsAreLoopbackOnly(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()
	for _, path := range []string{"/desktop/qr.png", "/desktop/open-logs"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, lanRequest(http.MethodGet, path, ""))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from LAN = %d, want 403", path, rec.Code)
		}
	}
}

// A page on some website, open in a browser on this very computer, is a loopback
// caller. The cross-site guard is what stops it from driving pairing.
func TestDriveByPageCannotDrivePairing(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()

	req := httptest.NewRequest(http.MethodPost, "/desktop/pairing/unpair-all", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:60001"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin unpair = %d, want 403", rec.Code)
	}

	// A form post (no preflight, so no header to strip) is refused as well.
	req = httptest.NewRequest(http.MethodPost, "/desktop/pairing/unpair-all", strings.NewReader("nonce=x"))
	req.RemoteAddr = "127.0.0.1:60002"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("form-post unpair = %d, want 403", rec.Code)
	}
}

// The QR image must stop carrying the secret while pairing is locked, so that a
// photo taken during a lockout is useless after the refresh.
func TestPairingURLOmitsSecretWhileLocked(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := New(player.New())
	auth.Default.Refresh()

	url := s.pairingURL()
	if !strings.Contains(url, "/#p="+auth.Default.Secret()) {
		t.Fatalf("open pairing should encode the secret, got %q", url)
	}

	for i := 0; i < 10; i++ {
		_, _ = auth.Default.PairWithSecret("ZZZZZZZZZZZZZZZZ", "")
	}
	if !auth.Default.Locked() {
		t.Fatal("expected pairing to lock")
	}
	if locked := s.pairingURL(); strings.Contains(locked, "#p=") {
		t.Fatalf("locked pairing still encodes a secret: %q", locked)
	}
	auth.Default.Refresh()
}
