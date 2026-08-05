package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"tvremote/internal/player"
)

func contractResponse(t *testing.T, handler http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d: %s", path, rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s decode: %v", path, err)
	}
	return body
}

func TestCapabilitiesDescribeVersionedDesktopContract(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := New(player.New())
	s.SetVersion("1.2.3")

	body := contractResponse(t, testHandler(s), "/api/capabilities")
	protocol := body["protocol"].(map[string]any)
	if protocol["major"] != float64(remoteProtocolMajor) ||
		protocol["minor"] != float64(remoteProtocolMinor) {
		t.Fatalf("protocol = %#v", protocol)
	}
	device := body["device"].(map[string]any)
	if device["id"] == "" || device["name"] == "" {
		t.Fatalf("device identity is incomplete: %#v", device)
	}
	if device["app_version"] != "1.2.3" {
		t.Fatalf("app_version = %v", device["app_version"])
	}
	if got := body["source_types"].([]any); len(got) != len(desktopSourceTypes) {
		t.Fatalf("source_types = %#v", got)
	}
}

func TestDesktopTargetNeverGrantsAppleEntitlement(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	body := contractResponse(t, testHandler(New(player.New())), "/api/entitlements")
	if body["product_id"] != proProductID ||
		body["target_pro"] != false ||
		body["grants_paired_controllers"] != false {
		t.Fatalf("entitlements = %#v", body)
	}
}

func TestNativeContractRequiresPairingOffLoopback(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	h := New(player.New()).Handler()
	for _, path := range []string{"/api/capabilities", "/api/entitlements"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.1:54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, rec.Code)
		}
	}
}
