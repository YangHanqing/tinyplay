package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopPageIncludesLocalizedNetworkGuidance(t *testing.T) {
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop?lang=zh-CN", nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "手机无法打开此地址？") {
		t.Fatalf("missing localized network help: %s", body)
	}
	if !strings.Contains(body, "本地网络") && runtime.GOOS == "darwin" {
		t.Fatalf("missing macOS local-network guidance: %s", body)
	}
}

func TestDesktopPageShowsMacOSLocalNetworkDenial(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the precise denial UI is only rendered by the macOS shell")
	}
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop?lang=en&local_network=denied", nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)

	if !strings.Contains(rec.Body.String(), "Local Network access is turned off") {
		t.Fatalf("missing local-network-denied notice: %s", rec.Body.String())
	}
}
