package server

import (
	_ "embed"
	"fmt"
	"net/http"
	"runtime"

	"github.com/skip2/go-qrcode"

	"tvremote/internal/config"
	"tvremote/internal/dlna"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
)

//go:embed assets/carina_nebula.jpg
var desktopBackgroundJPG []byte

// phoneURL is the address the phone should open, derived from the LAN IP and
// the port the server actually bound to (falls back to the configured port if
// SetPort was never called, e.g. in tests).
func (s *Server) phoneURL() string {
	port := s.port
	if port == 0 {
		port = config.Load().ListenPort
	}
	return fmt.Sprintf("http://%s:%d", netutil.LocalIP(), port)
}

// desktopPage is the intro + QR shown in the native shell's window. The macOS
// AppKit shell and the Windows WebView2 shell both load this page. Compact is
// the default; a Full Screen control expands the same window into an HTPC
// standby idle screen.
func (s *Server) desktopPage(w http.ResponseWriter, r *http.Request) {
	url := s.phoneURL()
	lang := i18n.RequestLang(r)
	help := i18n.T(lang, desktopNetworkHelpKey())
	denied := runtime.GOOS == "darwin" && r.URL.Query().Get("local_network") == "denied"
	notices := desktopNotices(lang, denied, player.DetectMPV().Available)
	standbyDLNA := s.desktopStandbyDLNA(lang)
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>TinyPlay</title>
<link rel="icon" href="/static/favicon.ico" sizes="any">
<style>
  :root {
    color-scheme: light dark;
    --fg: CanvasText;
    --muted: color-mix(in srgb, CanvasText 70%%, transparent);
    --panel: rgba(127,127,127,.12);
    --panel-strong: rgba(0,0,0,.42);
    --accent: #8ec8ff;
    --btn-bg: rgba(127,127,127,.16);
    --btn-border: rgba(127,127,127,.28);
  }
  * { box-sizing: border-box; }
  html, body { height: 100%%; }
  body {
    font-family: -apple-system, "Segoe UI", system-ui, sans-serif;
    margin: 0; min-height: 100vh; color: var(--fg);
    background: Canvas;
  }
  button {
    font: inherit; cursor: pointer; color: inherit;
    border: 1px solid var(--btn-border); background: var(--btn-bg);
    border-radius: 999px; padding: 9px 16px;
  }
  button:hover { filter: brightness(1.08); }
  button:active { transform: translateY(1px); }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

  /* —— Compact mode (default intro/QR) —— */
  .compact {
    min-height: 100vh; display: flex; flex-direction: column;
    align-items: center; justify-content: flex-start; gap: 12px;
    text-align: center; padding: 20px 16px 12px;
  }
  .compact-title {
    width: 100%%; min-height: 32px;
    display: flex; align-items: center; justify-content: center;
  }
  .compact h1 { font-size: 22px; margin: 0; letter-spacing: .01em; }
  .compact .intro { margin: 0; opacity: .68; max-width: 340px; line-height: 1.45; font-size: 14px; }
  .compact .qr {
    width: 220px; height: 220px; border-radius: 12px; background: #fff; padding: 12px;
  }
  .compact-qr-stage {
    flex: 1 1 220px; min-height: 220px; width: 100%%;
    display: flex; align-items: center; justify-content: center;
  }
  .compact code {
    font-size: 14px; padding: 4px 10px; border-radius: 6px;
    background: rgba(127,127,127,.15); word-break: break-all;
  }
  .notice {
    width: min(320px, calc(100vw - 40px)); text-align: left; border-radius: 10px;
    padding: 12px 14px; background: var(--panel); font-size: 13px; line-height: 1.5;
  }
  .notice { display: grid; gap: 5px; }
  .notice.error { background: rgba(218,54,51,.16); color: #a61b1b; }
  .notice strong { font-size: 14px; }
  .status-pill {
    display: inline-flex; align-items: center; gap: 7px; padding: 6px 10px;
    border: 1px solid rgba(127,127,127,.24); border-radius: 999px;
    font-size: 13px; font-weight: 600;
  }
  .status-dot { width: 8px; height: 8px; border-radius: 50%%; background: #86868b; }
  .status-pill.available { color: #167442; }
  .status-pill.available .status-dot { background: #24a461; box-shadow: 0 0 0 3px rgba(36,164,97,.16); }
  .status-pill.unavailable { color: #b42318; }
  .status-pill.unavailable .status-dot { background: #dc2626; box-shadow: 0 0 0 3px rgba(220,38,38,.14); }
  .fullscreen-action {
    width: 100%%; display: flex; align-items: center; justify-content: center;
  }
  .fs-enter {
    font-size: 14px; font-weight: 600; min-width: 148px;
  }
  .network-help {
    width: auto; max-width: min(320px, calc(100vw - 40px));
    padding: 0; overflow: hidden; color: var(--muted);
    font-size: 11px; line-height: 1.35; opacity: .62;
  }
  .network-help summary {
    cursor: pointer; list-style: none; padding: 2px 8px; font-weight: 500;
    text-align: center;
  }
  .network-help summary::-webkit-details-marker { display: none; }
  .network-help summary::marker { content: ""; }
  .network-help summary:hover { color: var(--fg); text-decoration: underline; }
  .network-help[open] {
    width: min(320px, calc(100vw - 40px)); padding: 8px 10px 10px;
    border-radius: 10px; background: var(--panel); opacity: .82;
  }
  .network-help[open] summary { padding: 0 4px 6px; font-size: 12px; }
  .network-help p { max-width: none; margin: 0; font-size: 12px; line-height: 1.45; }

  /* —— Standby / full-screen mode —— */
  .standby {
    display: none; position: fixed; inset: 0; overflow: hidden;
    color: #f4f7fb;
  }
  .standby-bg {
    position: absolute; inset: 0;
    background: #05070c center / cover no-repeat;
    background-image: url("/desktop/background.jpg");
    transform: scale(1.02);
    filter: saturate(1.05) brightness(.88);
  }
  .standby-scrim {
    position: absolute; inset: 0;
    background:
      radial-gradient(ellipse 90%% 70%% at 50%% 42%%, rgba(0,0,0,.18), transparent 70%%),
      linear-gradient(180deg, rgba(4,8,16,.55) 0%%, rgba(4,8,16,.28) 38%%, rgba(4,8,16,.52) 72%%, rgba(2,4,10,.78) 100%%);
  }
  .standby-inner {
    position: relative; z-index: 1; min-height: 100%%;
    display: grid; grid-template-rows: auto 1fr auto;
    padding: clamp(20px, 4vh, 48px) clamp(20px, 5vw, 64px);
    gap: clamp(12px, 2vh, 24px);
  }
  .standby-top {
    display: flex; align-items: flex-start; justify-content: space-between; gap: 16px;
  }
  .brand {
    display: grid; gap: 10px; text-align: left;
  }
  .brand-heading {
    display: flex; align-items: baseline; gap: 14px;
  }
  .brand-name {
    font-size: clamp(22px, 2.4vw, 34px); font-weight: 700; letter-spacing: .02em; margin: 0;
  }
  .brand-ready {
    margin: 0; font-size: clamp(13px, 1.2vw, 16px); letter-spacing: .14em;
    text-transform: uppercase; color: rgba(232,240,255,.72); font-weight: 600;
  }
  .standby-dlna {
    display: inline-flex; align-items: center; gap: 8px; width: fit-content;
    padding: 7px 11px; border: 1px solid rgba(255,255,255,.14);
    border-radius: 999px; background: rgba(5,9,16,.24);
    color: rgba(241,246,255,.82); backdrop-filter: blur(10px);
    font-size: clamp(12px, 1.05vw, 15px); font-weight: 600;
  }
  .standby-dlna .status-dot { background: #35c777; box-shadow: 0 0 0 3px rgba(53,199,119,.16); }
  .standby-dlna.unavailable { color: rgba(255,222,219,.9); }
  .standby-dlna.unavailable .status-dot { background: #ff665d; box-shadow: 0 0 0 3px rgba(255,102,93,.16); }
  .fs-exit {
    background: rgba(255,255,255,.08); border-color: rgba(255,255,255,.18);
    color: #f4f7fb; backdrop-filter: blur(8px); font-weight: 600;
    white-space: nowrap;
  }
  .standby-main {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    text-align: center; min-height: 0;
  }
  .clock {
    font-variant-numeric: tabular-nums;
    font-size: clamp(56px, 9vw, 120px); font-weight: 300; letter-spacing: .02em;
    line-height: 1; margin: 0; text-shadow: 0 2px 24px rgba(0,0,0,.35);
  }
  .clock-date {
    margin: 8px 0 0; font-size: clamp(14px, 1.5vw, 20px);
    color: rgba(232,240,255,.72); font-weight: 500; letter-spacing: .04em;
  }
  .standby-footer {
    display: flex; align-items: flex-end; justify-content: space-between; gap: 24px;
  }
  .standby-qr {
    display: block; width: clamp(112px, 9vw, 156px); height: clamp(112px, 9vw, 156px);
    border-radius: 12px; background: #fff; padding: 8px;
    box-shadow: 0 12px 40px rgba(0,0,0,.28);
  }
  .photo-credit {
    margin: 0; font-size: 12px; letter-spacing: .02em;
    color: rgba(232,240,255,.48); text-shadow: 0 1px 8px rgba(0,0,0,.4);
  }

  body.mode-standby { background: #05070c; }
  body.mode-standby .compact { display: none; }
  body.mode-standby .standby { display: block; }

  @media (max-width: 700px), (max-height: 500px) {
    .standby-top { flex-wrap: wrap; }
    .standby-qr { width: 96px; height: 96px; }
  }
  /* The Windows host's compact content area is shorter than its 360x560
     outer window because of the title bar. Keep the new full-screen action
     visible without making the intro window itself larger. */
  @media (max-width: 500px) and (max-height: 620px) {
    .compact { justify-content: flex-start; gap: 8px; padding: 12px 12px 14px; }
    .compact h1 { font-size: 20px; }
    .compact .intro { font-size: 14px; line-height: 1.5; }
    .compact .qr { width: 200px; height: 200px; padding: 10px; }
    .compact code { font-size: 12px; }
    .status-pill { padding: 5px 9px; font-size: 12px; }
    .notice { font-size: 12px; }
    .compact-qr-stage { min-height: 200px; }
    .fs-enter { padding: 8px 14px; }
  }
  @media (prefers-reduced-motion: reduce) {
    .standby-bg { transform: none; }
  }
</style></head>
<body class="mode-compact">
  <div class="compact">
    <div class="compact-title">
      <h1>TinyPlay</h1>
    </div>
    <p class="intro">%s</p>
    <div class="compact-qr-stage">
      <img class="qr" src="/desktop/qr.png" alt="QR" width="220" height="220">
    </div>
    <code id="url-compact">%s</code>
    <div id="dlna-status">%s</div>
    %s
    <div class="fullscreen-action">
      <button type="button" class="fs-enter" id="fs-enter" aria-pressed="false">%s</button>
    </div>
    %s
  </div>

  <div class="standby" id="standby" aria-hidden="true">
    <div class="standby-bg" role="presentation"></div>
    <div class="standby-scrim" role="presentation"></div>
    <div class="standby-inner">
      <div class="standby-top">
        <div class="brand">
          <div class="brand-heading">
            <h1 class="brand-name">TinyPlay</h1>
            <p class="brand-ready">%s</p>
          </div>
          %s
        </div>
        <button type="button" class="fs-exit" id="fs-exit">%s</button>
      </div>
      <div class="standby-main">
        <div>
          <p class="clock" id="clock" aria-live="polite">--:--</p>
          <p class="clock-date" id="clock-date"></p>
        </div>
      </div>
      <div class="standby-footer">
        <img class="standby-qr" src="/desktop/qr.png" alt="QR" width="148" height="148">
        <p class="photo-credit">%s</p>
      </div>
    </div>
  </div>

  <script>
    (() => {
      const body = document.body;
      const standby = document.getElementById('standby');
      const enterBtn = document.getElementById('fs-enter');
      const exitBtn = document.getElementById('fs-exit');
      const clockEl = document.getElementById('clock');
      const dateEl = document.getElementById('clock-date');
      const lang = document.documentElement.lang || 'en';
      let wantStandby = false;
      let nativeBridge = false;

      const setLayout = (standbyOn) => {
        body.classList.toggle('mode-standby', standbyOn);
        body.classList.toggle('mode-compact', !standbyOn);
        standby.setAttribute('aria-hidden', standbyOn ? 'false' : 'true');
        enterBtn.setAttribute('aria-pressed', standbyOn ? 'true' : 'false');
      };

      // Called by the native shell after true window full-screen changes.
      window.__tinyplayNativeFullscreen = (on) => {
        nativeBridge = true;
        wantStandby = !!on;
        setLayout(wantStandby);
      };

      const hasNativeBridge = () =>
        typeof window.tinyplaySetFullscreen === 'function' ||
        !!(window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.tinyplaySetFullscreen);

      const requestNative = async (enter) => {
        if (typeof window.tinyplaySetFullscreen === 'function') {
          await window.tinyplaySetFullscreen(!!enter);
          return true;
        }
        const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.tinyplaySetFullscreen;
        if (handler) {
          handler.postMessage(!!enter);
          return true;
        }
        return false;
      };

      const requestDomFullscreen = async (enter) => {
        try {
          if (enter) {
            const el = document.documentElement;
            if (el.requestFullscreen) await el.requestFullscreen();
            else if (el.webkitRequestFullscreen) el.webkitRequestFullscreen();
          } else if (document.fullscreenElement || document.webkitFullscreenElement) {
            if (document.exitFullscreen) await document.exitFullscreen();
            else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
          }
        } catch (_) {}
      };

      const setStandby = async (enter) => {
        wantStandby = !!enter;
        // Optimistic layout so the HTPC screen paints even if the shell is slow.
        setLayout(wantStandby);
        if (await requestNative(enter)) return;
        await requestDomFullscreen(enter);
        if (!document.fullscreenElement && !document.webkitFullscreenElement && enter) {
          // Browser denied DOM fullscreen; keep visual standby in-page.
          setLayout(true);
        }
      };

      enterBtn.addEventListener('click', () => setStandby(true));
      exitBtn.addEventListener('click', () => setStandby(false));

      document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && wantStandby) {
          e.preventDefault();
          setStandby(false);
        }
      });

      const onFsChange = () => {
        if (nativeBridge || hasNativeBridge()) return;
        const on = !!(document.fullscreenElement || document.webkitFullscreenElement);
        wantStandby = on;
        setLayout(on);
      };
      document.addEventListener('fullscreenchange', onFsChange);
      document.addEventListener('webkitfullscreenchange', onFsChange);

      const pad = (n) => String(n).padStart(2, '0');
      const tick = () => {
        const now = new Date();
        clockEl.textContent = pad(now.getHours()) + ':' + pad(now.getMinutes());
        try {
          dateEl.textContent = new Intl.DateTimeFormat(lang, {
            weekday: 'long', year: 'numeric', month: 'long', day: 'numeric'
          }).format(now);
        } catch (_) {
          dateEl.textContent = now.toDateString();
        }
      };
      tick();
      setInterval(tick, 1000);

      // If the native window is already full screen (page reloaded while
      // standby was active), restore the standby layout immediately.
      (async () => {
        try {
          if (typeof window.tinyplayIsFullscreen === 'function') {
            const on = await window.tinyplayIsFullscreen();
            if (on) window.__tinyplayNativeFullscreen(true);
          }
        } catch (_) {}
        if (document.fullscreenElement || document.webkitFullscreenElement) {
          window.__tinyplayNativeFullscreen(true);
        }
      })();

      const status = document.getElementById('dlna-status');
      let enabled = status.childElementCount > 0;
      setInterval(async () => {
        try {
          const settings = await fetch('/api/settings', { cache: 'no-store' }).then(r => r.json());
          const next = settings.dlna_receiver_enabled !== false;
          if (next !== enabled) location.reload();
        } catch (_) {}
      }, 3000);
    })();
  </script>
</body></html>`,
		lang,
		i18n.T(lang, "desktop_intro"),
		url,
		s.desktopDLNAStatus(lang),
		notices,
		i18n.T(lang, "desktop_fullscreen"),
		desktopNetworkHelp(denied, lang, help),
		i18n.T(lang, "desktop_standby_ready"),
		standbyDLNA,
		i18n.T(lang, "desktop_exit_fullscreen"),
		i18n.T(lang, "desktop_photo_credit"),
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// desktopBackground serves the bundled HTPC standby backdrop. Immutable cache
// is safe because the asset is compiled into the binary and only changes with
// a new build.
func (s *Server) desktopBackground(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(desktopBackgroundJPG)))
	w.Write(desktopBackgroundJPG)
}

// desktopDLNAStatus makes discovery health visible before the user tries to
// cast. The red state is intentionally about the actual SSDP socket, not mpv:
// another DLNA app may have made UDP 1900 unavailable.
func (s *Server) desktopDLNAStatus(lang string) string {
	status := s.dlnaReceiverStatus()
	if status == "disabled" {
		return ""
	}
	label := i18n.T(lang, "desktop_dlna_unavailable")
	if status == "available" {
		label = i18n.T(lang, "desktop_dlna_available", dlna.FriendlyName())
	}
	return fmt.Sprintf(`<span class="status-pill %s" role="status"><span class="status-dot"></span>%s</span>`, status, label)
}

// desktopStandbyDLNA mirrors the receiver name shown in phone casting menus.
// It stays out of the idle screen entirely when DLNA is disabled.
func (s *Server) desktopStandbyDLNA(lang string) string {
	status := s.dlnaReceiverStatus()
	if status == "disabled" {
		return ""
	}
	label := "DLNA · " + dlna.FriendlyName()
	if status == "unavailable" {
		label = i18n.T(lang, "desktop_dlna_short") + " · " + dlna.FriendlyName()
	}
	return fmt.Sprintf(`<div class="standby-dlna %s" role="status"><span class="status-dot"></span><span>%s</span></div>`, status, label)
}

// desktopNotices renders only actionable failures. A configured DLNA receiver
// still answers discovery without mpv, so surface the missing runtime before a
// phone sender reaches a generic casting error.
func desktopNotices(lang string, denied, mpvAvailable bool) string {
	notices := ""
	if denied {
		notices += fmt.Sprintf(`<section class="notice error" role="alert"><strong>%s</strong><span>%s</span></section>`,
			i18n.T(lang, "desktop_network_denied_title"), i18n.T(lang, "desktop_network_denied_body"))
	}
	if !mpvAvailable {
		notices += fmt.Sprintf(`<section class="notice error" role="alert"><strong>%s</strong><span>%s</span></section>`,
			i18n.T(lang, "desktop_mpv_missing_title"), i18n.T(lang, "desktop_mpv_missing_body"))
	}
	return notices
}

// desktopNetworkHelp keeps troubleshooting out of the normal scanning flow.
// A macOS Local Network denial is a confirmed condition and already has a
// visible alert above; otherwise, expose platform-specific steps only when the
// person explicitly opens this compact disclosure. Windows does not offer an
// equivalent permission-status API for inbound firewall rules, so showing an
// error there without a failed connection would be misleading.
func desktopNetworkHelp(denied bool, lang, help string) string {
	if denied {
		return ""
	}
	return fmt.Sprintf(`<details class="network-help"><summary>%s</summary><p>%s</p></details>`,
		i18n.T(lang, "desktop_network_help_title"), help)
}

func desktopNetworkHelpKey() string {
	switch runtime.GOOS {
	case "darwin":
		return "desktop_network_help_macos"
	case "windows":
		return "desktop_network_help_windows"
	default:
		return "desktop_network_help_generic"
	}
}

// desktopQR renders the phone URL as a PNG QR code.
func (s *Server) desktopQR(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(s.phoneURL(), qrcode.Medium, 480)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
