package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/player"
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

// TestDesktopPageCrossPlatformShellContract locks the bridge names and footer
// affordances that both the macOS and Windows shells must implement so the
// shared /desktop page behaves the same on each OS.
func TestDesktopPageCrossPlatformShellContract(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop?lang=en&version=1.2.3", nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)
	body := rec.Body.String()

	// Shared native bridge names (Windows Bind / macOS WKScriptMessageHandler).
	for _, bridge := range []string{
		"tinyplaySetFullscreen",
		"tinyplayIsFullscreen",
		"tinyplayShowAbout",
		"tinyplayCheckForUpdates",
		"__tinyplayUpdateCheckDone",
		"__tinyplayNativeFullscreen",
	} {
		if !strings.Contains(body, bridge) {
			t.Fatalf("shared /desktop page missing cross-platform bridge %q", bridge)
		}
	}
	// Version doubles as About; check-updates has a spinner; no second About link.
	if !strings.Contains(body, `id="btn-about"`) || !strings.Contains(body, ">v1.2.3<") {
		t.Fatal("version footer must be the clickable About control (v1.2.3)")
	}
	if strings.Count(body, `id="btn-about"`) != 1 {
		t.Fatal("expected a single About control (the version label)")
	}
	if !strings.Contains(body, `id="btn-check-updates"`) || !strings.Contains(body, "footer-spinner") {
		t.Fatal("check-for-updates must expose the same loading spinner on both platforms")
	}
	// Ready pill uses the same status-dot language as DLNA on every OS.
	if !strings.Contains(body, `status-pill available compact-ready`) ||
		!strings.Contains(body, "text-transform: uppercase") {
		t.Fatal("ready pill + uppercase status labels must be in the shared page CSS")
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
		`class="compact-columns"`,
		`class="brand-logo"`,
		`class="brand-core"`,
		`class="connect-card"`,
		`class="compact-qr-stage"`,
		`class="fullscreen-action"`,
		`class="url-row"`,
		`class="network-help"`,
		`class="standby-qr"`,
		"color-scheme: light",
		"--bg: #f7f9fc",
		"border-right: 1px solid var(--divider)",
		"compact-ready",
		"footer-spinner",
		"__tinyplayUpdateCheckDone",
		"DLNA unavailable · TinyPlay (",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("desktop page missing %q", want)
		}
	}
	// Compact is logo-only on the left; standby still shows the TinyPlay wordmark.
	// Ready appears on compact (beside URL) and on standby (top chrome).
	if n := strings.Count(body, `class="brand-name"`); n < 1 {
		t.Fatalf("expected TinyPlay wordmark on standby, found %d", n)
	}
	if strings.Contains(body, `<h1 class="brand-name">TinyPlay</h1>`) &&
		strings.Index(body, `<h1 class="brand-name">TinyPlay</h1>`) < strings.Index(body, `class="connect-card"`) {
		// Only fail if a compact-side wordmark appears before the connect card.
		compactSideEnd := strings.Index(body, `class="compact-main"`)
		wordmark := strings.Index(body, `<h1 class="brand-name">TinyPlay</h1>`)
		if compactSideEnd > 0 && wordmark >= 0 && wordmark < compactSideEnd {
			t.Fatal("compact left column should keep logo only (no TinyPlay wordmark)")
		}
	}
	if !strings.Contains(body, `class="status-pill available compact-ready"`) ||
		!strings.Contains(body, `class="status-dot"`) {
		t.Fatal("compact ready capsule should use the same status-pill + status-dot as DLNA")
	}
	if !strings.Contains(body, "text-transform: uppercase") {
		t.Fatal("status pills should uppercase Ready labels for Latin scripts")
	}
	if !strings.Contains(body, `id="btn-about"`) || !strings.Contains(body, `id="btn-check-updates"`) {
		t.Fatal("footer should keep version→about and check-for-updates actions")
	}
	// Compact is bright; standby keeps its own night canvas.
	if !strings.Contains(body, "body.mode-standby") || !strings.Contains(body, "#05070c") {
		t.Fatal("standby mode must keep the night canvas independent of compact light theme")
	}
	fullscreenIndex := strings.Index(body, `id="fs-enter"`)
	helpIndex := strings.Index(body, `class="network-help"`)
	if fullscreenIndex < 0 || helpIndex < 0 || fullscreenIndex > helpIndex {
		t.Fatal("compact Full Screen action must appear above the small network-help disclosure")
	}
	// Left column: logo only, centered. Full Screen sits under the QR.
	// Intro sits above the URL; ready capsule is to the right of the URL.
	logoIndex := strings.Index(body, `class="brand-logo"`)
	qrIndex := strings.Index(body, `class="compact-qr-stage"`)
	introIndex := strings.Index(body, `class="intro"`)
	urlIndex := strings.Index(body, `id="url-compact"`)
	readyIndex := strings.Index(body, `class="status-pill available compact-ready"`)
	if logoIndex < 0 || qrIndex < 0 || fullscreenIndex < 0 || !(qrIndex < fullscreenIndex) {
		t.Fatal("full-screen action should sit under the QR code")
	}
	if introIndex < 0 || urlIndex < 0 || !(introIndex < urlIndex) {
		t.Fatal("intro copy should sit above the phone URL next to the QR")
	}
	if readyIndex < 0 || urlIndex > readyIndex {
		t.Fatal("ready capsule should appear immediately after the phone URL")
	}
	if !strings.Contains(body, "border-radius: 999px") || !strings.Contains(body, ".status-pill.available") {
		t.Fatal("ready status beside the URL should be a green status pill")
	}
	if !strings.Contains(body, ".brand-core") || !strings.Contains(body, "padding: 6px") {
		t.Fatal("expected centered logo core and tighter QR padding")
	}
	if !strings.Contains(body, `class="connect-qr-col"`) {
		t.Fatal("QR column should group the code and the full-screen control")
	}
	if !strings.Contains(body, `class="aux-spacer"`) {
		t.Fatal("aux rows should keep a flex spacer between label and trailing controls")
	}
	// Network help sits under the paired-devices row, not beside it.
	devicesIndex := strings.Index(body, `id="pair-devices"`)
	if devicesIndex < 0 || helpIndex < devicesIndex {
		t.Fatal("network-help disclosure should appear below the paired-devices row")
	}
	if !strings.Contains(body, "margin-top: auto") || !strings.Contains(body, "flex: 1 1 auto") {
		t.Fatal("compact layout should fill the shell height: grow the middle card and pin the footer")
	}
	if strings.Contains(body, "下方") || strings.Contains(body, "下の") {
		t.Fatal("intro copy must not say the QR is below the text")
	}
	// Intro should still be a short paragraph (not a single phrase, not the old essay).
	if !strings.Contains(body, i18n.T("en", "desktop_intro")) {
		t.Fatal("expected localized intro copy on the desktop page")
	}
	if intro := i18n.T("zh-CN", "desktop_intro"); len([]rune(intro)) < 20 || len([]rune(intro)) > 80 {
		t.Fatalf("zh-CN intro should be one or two short sentences, got %q (%d runes)", intro, len([]rune(intro)))
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
	notices := desktopNotices(lang, false, player.MPVInfo{Available: false})
	if !strings.Contains(notices, i18n.T(lang, "desktop_mpv_missing_title")) {
		t.Fatalf("missing mpv runtime warning: %s", notices)
	}
	if got := desktopNotices(lang, false, player.MPVInfo{Available: true}); got != "" {
		t.Fatalf("available mpv should not produce a warning: %s", got)
	}
	if got := desktopNotices(lang, false, player.MPVInfo{Available: true, CustomConfigured: true, CustomInvalid: true}); !strings.Contains(got, i18n.T(lang, "desktop_mpv_custom_invalid_title")) {
		t.Fatalf("stale custom mpv path should produce a warning: %s", got)
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

// The standby (HTPC idle) screen shows the same QR code as the compact view,
// but both pairing cards live in the compact tree — so standby used to display
// a bare QR with no way to understand or answer it. Two concrete failures came
// out of that: while QR pairing is locked, pairingURL() drops the secret and
// the standby code silently pairs nothing; and a phone waiting on four digits
// had no visible approve card. Lock the three mechanics that fix it.
func TestDesktopStandbyQRFollowsPairingState(t *testing.T) {
	s := &Server{port: 1980}
	req := httptest.NewRequest(http.MethodGet, "/desktop", nil)
	rec := httptest.NewRecorder()
	s.desktopPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		// The standby QR is wrapped so a locked state can swap it for prose.
		`id="standby-qr-stage"`,
		`id="standby-qr"`,
		`id="standby-pair-note"`,
		`.standby-qr-stage.is-locked .standby-qr { display: none; }`,
		// A refreshed secret has to reach both copies of the image.
		`if (standbyQR) standbyQR.src = src;`,
		// A consent request needs a human, and the card is compact-only.
		`if (pending && wantStandby) setStandby(false);`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered desktop page is missing %q", want)
		}
	}
	// The page is one large Sprintf; a mismatched placeholder would otherwise
	// only show up as garbled chrome in a window nobody has open.
	if strings.Contains(body, "%!s(") || strings.Contains(body, "%!(EXTRA") {
		t.Error("Sprintf placeholder mismatch in the desktop page")
	}
}
