package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
)

func TestDesktopPageIncludesLocalizedNetworkGuidance(t *testing.T) {
	const lang = "zh-CN"
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop?lang="+lang, nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, i18n.T(lang, "desktop_network_help_title")) {
		t.Fatalf("missing localized network help: %s", body)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(body, i18n.T(lang, "desktop_network_help_macos")) {
		t.Fatalf("missing macOS local-network guidance: %s", body)
	}
}

func TestDesktopPageIncludesFullscreenStandbyControls(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop?lang=en", nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"Full Screen",
		"Exit Full Screen",
		"Ready",
		"NASA, ESA, CSA, STScI",
		"/desktop/background.jpg",
		"tinyplaySetFullscreen",
		"tinyplayIsFullscreen",
		"mode-standby",
		`id="clock"`,
		`id="fs-enter"`,
		`id="fs-exit"`,
		`class="compact-title"`,
		`class="compact-qr-stage"`,
		`class="fullscreen-action"`,
		`class="network-help"`,
		`class="standby-qr"`,
		"DLNA unavailable · TinyPlay (",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("desktop page missing %q", want)
		}
	}
	fullscreenIndex := strings.Index(body, `id="fs-enter"`)
	helpIndex := strings.Index(body, `class="network-help"`)
	if fullscreenIndex < 0 || helpIndex < 0 || fullscreenIndex > helpIndex {
		t.Fatal("compact Full Screen action must appear above the small network-help disclosure")
	}
	for _, unwanted := range []string{`id="url-standby"`, "Scan the QR code with your phone to control playback", `<strong>LAN</strong>`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("standby page should not contain %q", unwanted)
		}
	}
}

func TestDesktopBackgroundServesBundledJPEG(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/desktop/background.jpg", nil)
	rec := httptest.NewRecorder()
	s.desktopBackground(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("expected immutable cache header, got %q", cc)
	}
	body := rec.Body.Bytes()
	if len(body) < 50_000 || len(body) > 500_000 {
		t.Fatalf("unexpected background size: %d bytes", len(body))
	}
	// JPEG SOI marker
	if len(body) < 2 || body[0] != 0xff || body[1] != 0xd8 {
		t.Fatalf("response is not a JPEG")
	}
	if len(desktopBackgroundJPG) == 0 {
		t.Fatal("embedded background asset is empty")
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
	if strings.Contains(rec.Body.String(), "Can't connect from your phone?") {
		t.Fatalf("manual troubleshooting should not repeat a confirmed denial: %s", rec.Body.String())
	}
}

func TestDesktopNoticesIncludeMissingMPV(t *testing.T) {
	const lang = "zh-CN"
	notices := desktopNotices(lang, false, false)
	if !strings.Contains(notices, i18n.T(lang, "desktop_mpv_missing_title")) {
		t.Fatalf("missing mpv runtime warning: %s", notices)
	}
	if got := desktopNotices(lang, false, true); got != "" {
		t.Fatalf("available mpv should not produce a warning: %s", got)
	}
}

func TestDesktopDLNAStatusReflectsLiveReceiverState(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := &Server{}

	if got := s.desktopDLNAStatus("en"); !strings.Contains(got, "status-pill unavailable") || !strings.Contains(got, "UDP 1900") {
		t.Fatalf("enabled receiver without a live socket should be unavailable: %s", got)
	}
	config.SetDLNAReceiverEnabled(false)
	if got := s.desktopDLNAStatus("en"); got != "" {
		t.Fatalf("disabled receiver should not appear on the desktop page: %s", got)
	}
}
