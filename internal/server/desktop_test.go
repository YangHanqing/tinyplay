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
		"asset=ngc6000",
		"asset=earthrise",
		"nextIndex % 3",
		"IMAGE_HOLD_MS = 240000",
		"HUD_INITIAL_MS = 120000",
		"HUD_WAKE_MS = 20000",
		"IDLE_SLEEP_MS = 1800000",
		"is-sleeping",
		"hud-hidden",
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
	if strings.Contains(body, "%!") {
		t.Fatalf("desktop page contains a fmt formatting error: %s", body)
	}
}

func TestDesktopBackgroundServesBundledJPEG(t *testing.T) {
	s := &Server{}
	for _, path := range []string{
		"/desktop/background.jpg",
		"/desktop/background.jpg?asset=ngc6000",
		"/desktop/background.jpg?asset=earthrise",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.desktopBackground(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Fatalf("%s: Content-Type = %q", path, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Fatalf("%s: expected immutable cache header, got %q", path, cc)
		}
		body := rec.Body.Bytes()
		if len(body) < 50_000 {
			t.Fatalf("%s: unexpected background size: %d bytes", path, len(body))
		}
		// JPEG SOI marker
		if len(body) < 2 || body[0] != 0xff || body[1] != 0xd8 {
			t.Fatalf("%s: response is not a JPEG", path)
		}
	}
	if len(desktopBackgroundJPG) == 0 || len(desktopNGC6000JPG) == 0 || len(desktopEarthriseJPG) == 0 {
		t.Fatal("an embedded background asset is empty")
	}
}

func TestDesktopBackgroundRejectsUnknownAsset(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/desktop/background.jpg?asset=unknown", nil)
	rec := httptest.NewRecorder()
	s.desktopBackground(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
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
