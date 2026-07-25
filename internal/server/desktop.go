package server

import (
	_ "embed"
	"fmt"
	"net/http"
	"runtime"

	"github.com/skip2/go-qrcode"

	"tvremote/internal/auth"
	"tvremote/internal/config"
	"tvremote/internal/desktopinput"
	"tvremote/internal/dlna"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
)

//go:embed assets/carina_nebula.jpg
var desktopBackgroundJPG []byte

//go:embed assets/ngc6000.jpg
var desktopNGC6000JPG []byte

//go:embed assets/earthrise_lro.jpg
var desktopEarthriseJPG []byte

type desktopBackgroundAsset struct {
	image []byte
}

// Keep this set local and deliberately small: a responsive, offline standby
// screen must not become a video download. Credits and source records live in
// THIRD_PARTY_NOTICES.md beside the distributable desktop implementation.
var desktopBackgroundAssets = map[string]desktopBackgroundAsset{
	"carina":    {image: desktopBackgroundJPG},
	"ngc6000":   {image: desktopNGC6000JPG},
	"earthrise": {image: desktopEarthriseJPG},
}

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
// AppKit shell and the Windows WebView2 shell both load this exact page (same
// HTML/CSS/JS) over loopback with ?lang=&version=. Platform differences stay
// in the shells only:
//
//   • Window chrome size: both target ~900×540 content (Windows outer height
//     is larger by the title-bar allowance).
//   • Native bridges (same names on both sides):
//       tinyplaySetFullscreen      — Win Bind / mac messageHandler
//       tinyplayIsFullscreen       — Win Bind; mac re-syncs via didFinish instead
//       tinyplayShowAbout          — version footer click
//       tinyplayCheckForUpdates    — footer; both shells then call
//                                    window.__tinyplayUpdateCheckDone()
//       tinyplayRestart            — macOS: relaunch after Accessibility grant
//   • macOS-only: ?local_network=denied for Local Network permission help.
//
// Compact is the default; Full Screen expands the same window into an HTPC
// standby idle screen.
func (s *Server) desktopPage(w http.ResponseWriter, r *http.Request) {
	url := s.phoneURL()
	lang := i18n.RequestLang(r)
	help := i18n.T(lang, desktopNetworkHelpKey())
	denied := runtime.GOOS == "darwin" && r.URL.Query().Get("local_network") == "denied"
	notices := desktopNotices(lang, denied, player.DetectMPV().Available)
	standbyDLNA := s.desktopStandbyDLNA(lang)
	dlnaSection := s.desktopDLNASection(lang)
	inputSection := desktopInputSection(lang)
	footerSection := desktopFooterSection(lang, r.URL.Query().Get("version"))
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>TinyPlay</title>
<link rel="icon" href="/static/favicon.ico" sizes="any">
<style>
  /* Compact defaults to a bright, simple surface. Standby re-scopes to a
     night palette when body.mode-standby is active (see below). */
  :root {
    color-scheme: light;
    --bg: #f7f9fc;
    --fg: #141820;
    --muted: #5b6574;
    --muted-soft: #8a93a3;
    --panel: #ffffff;
    --panel-border: rgba(20,24,32,.10);
    --panel-border-soft: rgba(20,24,32,.07);
    --divider: rgba(20,24,32,.12);
    --accent: #2f8fff;
    --accent-ink: #ffffff;
    --btn-bg: #ffffff;
    --btn-border: rgba(20,24,32,.14);
    --success: #1f9d57;
    --success-text: #167442;
    --danger: #dc2626;
    --danger-text: #b42318;
    --shadow: 0 10px 28px rgba(20,24,32,.06);
  }
  * { box-sizing: border-box; }
  html, body { height: 100%%; }
  body {
    font-family: -apple-system, "Segoe UI", system-ui, sans-serif;
    margin: 0; min-height: 100vh; color: var(--fg);
    background: var(--bg);
    -webkit-font-smoothing: antialiased;
  }
  button {
    font: inherit; cursor: pointer; color: inherit;
    border: 1px solid var(--btn-border); background: var(--btn-bg);
    border-radius: 999px; padding: 9px 16px;
    transition: background .15s ease, border-color .15s ease, color .15s ease, transform .12s ease;
  }
  button:hover { background: #f1f4f8; border-color: rgba(20,24,32,.20); }
  button:active { transform: translateY(1px); }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  button:disabled { opacity: .5; cursor: default; }

  /* —— Compact mode (default intro/QR) —— */
  .compact {
    position: relative;
    /* Fill the fixed shell window; avoid a floating island with empty bands. */
    min-height: 100vh; height: 100%%; display: flex; flex-direction: column;
    align-items: stretch; justify-content: stretch;
    padding: 20px 28px 14px;
    background: var(--bg);
  }
  .compact-columns {
    flex: 1 1 auto; min-height: 0;
    display: flex; align-items: stretch;
    width: 100%%; max-width: none;
  }
  .compact-side {
    flex: 0 0 168px; display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    min-width: 0; min-height: 0;
    padding-right: 28px;
    border-right: 1px solid var(--divider);
  }
  /* Left column is logo-only, always centered. */
  .brand-core {
    display: flex; align-items: center; justify-content: center;
    width: 100%%;
  }
  .compact-main {
    flex: 1 1 auto; min-width: 0; min-height: 0;
    display: flex; flex-direction: column; gap: 14px;
    text-align: left; justify-content: flex-start;
    padding-left: 28px;
  }
  .brand-logo {
    width: 108px; height: 108px; display: block;
    border-radius: 26px;
    box-shadow:
      0 8px 22px rgba(20,24,32,.10),
      0 0 0 1px rgba(20,24,32,.06);
  }
  /* Ready beside the URL reuses the same pill + status-dot language as DLNA. */
  .status-pill.compact-ready {
    flex-shrink: 0;
    letter-spacing: .06em;
  }
  .compact .intro {
    margin: 0; color: var(--muted); max-width: 36ch;
    line-height: 1.5; font-size: 13.5px; font-weight: 500;
  }
  .connect-qr-col {
    flex: 0 0 auto;
    display: flex; flex-direction: column;
    align-items: center; gap: 12px;
  }
  .fullscreen-action {
    flex: 0 0 auto; width: 100%%;
    display: flex; align-items: center; justify-content: center;
  }
  .fs-enter {
    font-size: 13px; font-weight: 600; min-width: 0;
    background: var(--btn-bg); border-color: var(--btn-border);
    color: var(--fg); white-space: nowrap;
  }
  .url-row {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    width: 100%%;
  }
  .connect-card {
    flex: 1 1 auto; min-height: 0;
    display: flex; flex-direction: column;
    border-radius: 18px;
    border: 1px solid var(--panel-border);
    background: var(--panel);
    box-shadow: var(--shadow);
    padding: 22px 22px 18px;
  }
  .connect {
    flex: 1 1 auto; min-height: 0;
    display: flex; align-items: center; gap: 22px;
  }
  .connect-info {
    flex: 1 1 auto; min-width: 0;
    display: flex; flex-direction: column; gap: 12px; justify-content: center;
  }
  .compact-qr-stage {
    /* Fixed plate size: pairing UI swaps in place so the rest of the card
       does not reflow when a phone asks to pair. */
    flex: 0 0 auto; width: 188px; height: 188px;
    display: flex; align-items: stretch; justify-content: stretch;
    position: relative;
  }
  .compact .qr {
    width: 100%%; height: 100%%; border-radius: 14px; background: #fff;
    /* Padding halved from 12px so the code fills more of the white plate. */
    padding: 6px; flex-shrink: 0; display: block;
    box-shadow: 0 6px 18px rgba(20,24,32,.08);
    border: 1px solid var(--panel-border-soft);
  }
  .url-row code, .compact code {
    font-size: 13px; padding: 8px 12px; border-radius: 10px;
    background: #f1f4f8; border: 1px solid var(--panel-border-soft);
    color: var(--fg); word-break: break-all;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    letter-spacing: .01em;
  }
  .url-row code { min-width: 0; }
  .aux-list {
    display: flex; flex-direction: column;
    border-radius: 16px;
    border: 1px solid var(--panel-border);
    background: var(--panel);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .aux-list:empty { display: none; }
  .aux-row {
    display: flex; align-items: center; gap: 10px;
    padding: 11px 14px; border-bottom: 1px solid var(--panel-border-soft);
    font-size: 13px;
  }
  .aux-row:last-child { border-bottom: none; }
  .aux-row .aux-label { flex: 0 0 auto; font-weight: 600; color: var(--fg); }
  .aux-spacer { flex: 1 1 auto; min-width: 12px; }
  .aux-row .status-pill { flex-shrink: 0; }
  .switch {
    flex-shrink: 0; width: 38px; height: 22px; border-radius: 999px;
    background: #d5dae3; position: relative; border: none; padding: 0;
    cursor: pointer;
  }
  .switch::after {
    content: ""; position: absolute; top: 3px; left: 3px; width: 16px; height: 16px;
    border-radius: 50%%; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,.18);
    transition: transform .15s ease;
  }
  .switch.on { background: var(--success); }
  .switch.on::after { transform: translateX(16px); }
  .switch:disabled { opacity: .5; cursor: default; }
  .switch:hover { background: #c8ced9; border-color: transparent; }
  .switch.on:hover { background: #22ab62; }
  .aux-action {
    font-size: 12px; font-weight: 600; padding: 6px 12px; flex-shrink: 0;
  }
  .aux-actions { display: flex; flex-shrink: 0; align-items: center; gap: 8px; }
  .aux-hint {
    padding: 0 14px 11px; margin-top: -4px;
    font-size: 12px; line-height: 1.45; color: var(--muted);
  }
  .footer-row {
    display: flex; align-items: center; justify-content: space-between; gap: 16px;
    /* Pin under the two main blocks; flex:1 on connect-card fills the middle. */
    margin-top: auto; flex-shrink: 0;
    padding: 10px 4px 0; font-size: 12px; color: var(--muted-soft);
  }
  .footer-group { display: flex; align-items: center; gap: 14px; }
  .footer-link {
    border: none; background: none; padding: 0; font-size: 12px;
    color: var(--muted-soft); border-radius: 0; cursor: pointer;
    display: inline-flex; align-items: center; gap: 7px;
  }
  .footer-link:hover { color: var(--fg); text-decoration: underline; background: none; border-color: transparent; }
  .footer-link:disabled, .footer-link.is-checking {
    opacity: .55; cursor: default; text-decoration: none; pointer-events: none;
  }
  .footer-spinner {
    display: none; width: 11px; height: 11px; border-radius: 50%%;
    border: 1.5px solid rgba(20,24,32,.18);
    border-top-color: var(--muted);
    animation: footer-spin .7s linear infinite;
  }
  .footer-link.is-checking .footer-spinner { display: inline-block; }
  @keyframes footer-spin { to { transform: rotate(360deg); } }
  .notice {
    width: 100%%; text-align: left; border-radius: 14px;
    padding: 12px 14px; background: var(--panel);
    border: 1px solid var(--panel-border);
    font-size: 13px; line-height: 1.5;
    display: grid; gap: 5px;
  }
  .notice.error {
    background: rgba(220,38,38,.06);
    border-color: rgba(220,38,38,.18);
    color: var(--danger-text);
  }
  .notice strong { font-size: 14px; color: var(--fg); }
  .notice.error strong { color: #9f1239; }

  /* —— Pairing: replace the QR plate in place (no page reflow) —— */
  .compact-qr-stage.has-card {
    /* Same footprint as the QR plate; grow only if the copy needs a line more. */
    width: 200px; height: auto; min-height: 188px;
  }
  .compact-qr-stage.has-card .qr { display: none; }
  .pair-card {
    display: none;
    width: 100%%; min-height: 188px;
    box-sizing: border-box;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    text-align: center;
    border-radius: 14px;
    padding: 12px 10px 10px;
    background: #fff;
    border: 1px solid var(--panel-border-soft);
    box-shadow: 0 6px 18px rgba(20,24,32,.08);
    font-size: 12px; line-height: 1.35;
  }
  .pair-card.is-visible {
    display: flex;
    animation: pair-card-in .22s ease both;
  }
  @keyframes pair-card-in {
    from { opacity: 0; transform: scale(.97); }
    to { opacity: 1; transform: scale(1); }
  }
  .pair-card-title {
    margin: 0; font-size: 13px; font-weight: 700; color: var(--fg);
    letter-spacing: .01em;
  }
  .pair-card-hint {
    margin: 0; color: var(--muted); font-size: 11px; font-weight: 500;
    line-height: 1.35; max-width: 16em;
  }
  .pair-code {
    margin: 2px 0 0;
    font-size: 34px; font-weight: 700; letter-spacing: .24em;
    font-variant-numeric: tabular-nums; color: var(--fg);
    line-height: 1; padding-left: .24em; /* balance letter-spacing on last digit */
  }
  .pair-actions {
    display: flex; gap: 6px; justify-content: center;
    width: 100%%; margin-top: 4px;
  }
  .pair-actions button {
    flex: 1 1 0; min-width: 0;
    padding: 7px 8px; font-size: 12px; font-weight: 600;
    border-radius: 999px;
  }
  .pair-primary {
    color: var(--accent-ink);
    background: var(--accent); border-color: transparent;
  }
  .pair-primary:hover { background: #4a9fff; border-color: transparent; color: var(--accent-ink); }
  .pair-secondary {
    color: var(--muted); background: #f1f4f8; border-color: transparent;
  }
  .pair-secondary:hover { color: var(--fg); background: #e7ebf1; border-color: transparent; }
  .pair-card.is-locked .pair-actions button { flex: 1 1 auto; }
  .pair-row {
    display: flex; align-items: center; justify-content: flex-start;
    gap: 8px; flex-wrap: wrap; font-size: 12px; color: var(--muted);
    min-height: 18px;
  }
  /* A class rule outranks the UA stylesheet's [hidden] { display: none }. */
  .pair-row[hidden] { display: none; }
  .pair-link {
    border: none; background: none; padding: 2px 4px; font-size: 12px;
    color: var(--muted); text-decoration: underline; border-radius: 5px;
  }
  .pair-link:hover { color: var(--fg); background: none; border-color: transparent; }
  .pair-link.danger { color: var(--danger-text); }
  .pair-link.danger:hover { color: #9f1239; }
  .status-pill {
    display: inline-flex; align-items: center; gap: 7px; padding: 6px 10px;
    border: 1px solid var(--panel-border); border-radius: 999px;
    background: #f7f9fc;
    font-size: 12px; font-weight: 600; color: var(--muted);
    /* Latin "Ready" / "Listo" match the capsule; CJK is unaffected. */
    text-transform: uppercase;
  }
  .status-dot { width: 8px; height: 8px; border-radius: 50%%; background: #a0a8b5; flex-shrink: 0; }
  .status-pill.available { color: var(--success-text); border-color: rgba(31,157,87,.28); background: rgba(31,157,87,.08); }
  .status-pill.available .status-dot {
    background: var(--success); box-shadow: 0 0 0 3px rgba(31,157,87,.14);
  }
  .status-pill.unavailable { color: var(--danger-text); border-color: rgba(220,38,38,.24); background: rgba(220,38,38,.06); }
  .status-pill.unavailable .status-dot {
    background: var(--danger); box-shadow: 0 0 0 3px rgba(220,38,38,.12);
  }
  .network-help {
    width: 100%%; max-width: none;
    padding: 0; overflow: hidden; color: var(--muted-soft);
    font-size: 11px; line-height: 1.35;
  }
  .network-help summary {
    cursor: pointer; list-style: none; padding: 2px 0; font-weight: 500;
    text-align: left;
  }
  .network-help summary::-webkit-details-marker { display: none; }
  .network-help summary::marker { content: ""; }
  .network-help summary:hover { color: var(--fg); text-decoration: underline; }
  .network-help[open] {
    width: 100%%; padding: 10px 12px 12px;
    border-radius: 12px; background: #f7f9fc;
    border: 1px solid var(--panel-border);
  }
  .network-help[open] summary { padding: 0 2px 6px; font-size: 12px; color: var(--muted); }
  .network-help p { max-width: none; margin: 0; font-size: 12px; line-height: 1.45; color: var(--muted); }

  /* —— Standby / full-screen mode (night surface, independent of compact) —— */
  body.mode-standby {
    color-scheme: dark;
    --bg: #05070c;
    --fg: #f4f7fb;
    --muted: rgba(232,240,255,.72);
    --muted-soft: rgba(232,240,255,.48);
    --panel-border: rgba(255,255,255,.12);
    --success: #35c777;
    --danger: #ff665d;
    --danger-text: #ffc9c5;
    background: #05070c;
  }
  .standby {
    display: none; position: fixed; inset: 0; overflow: hidden;
    color: #f4f7fb;
  }
  .standby-bg {
    position: absolute; inset: 0;
    background: #05070c center / cover no-repeat;
    opacity: 0; filter: saturate(1.05) brightness(.88);
    transition: opacity 4s ease-in-out;
    will-change: opacity, transform;
  }
  .standby-bg.is-active { opacity: 1; }
  .standby-bg.motion-0 { animation: standby-pan-0 240s ease-out both; }
  .standby-bg.motion-1 { animation: standby-pan-1 240s ease-out both; }
  .standby-bg.motion-2 { animation: standby-pan-2 240s ease-out both; }
  @keyframes standby-pan-0 { from { transform: scale(1); } to { transform: scale(1.06) translate3d(-2%%, -1.5%%, 0); } }
  @keyframes standby-pan-1 { from { transform: scale(1.01) translate3d(-1%%, 1%%, 0); } to { transform: scale(1.06) translate3d(2%%, -1.5%%, 0); } }
  @keyframes standby-pan-2 { from { transform: scale(1) translate3d(1.5%%, -1%%, 0); } to { transform: scale(1.06) translate3d(-1.5%%, 1.5%%, 0); } }
  .standby-blackout {
    position: absolute; inset: 0; z-index: 3; background: #000; opacity: 0;
    pointer-events: none; transition: opacity 60s linear;
  }
  .standby-scrim {
    position: absolute; inset: 0;
    background:
      radial-gradient(ellipse 90%% 70%% at 50%% 42%%, rgba(0,0,0,.18), transparent 70%%),
      linear-gradient(180deg, rgba(4,8,16,.55) 0%%, rgba(4,8,16,.28) 38%%, rgba(4,8,16,.52) 72%%, rgba(2,4,10,.78) 100%%);
  }
  .standby-inner {
    position: relative; z-index: 2; min-height: 100%%;
    display: grid; grid-template-rows: auto 1fr auto;
    padding: clamp(20px, 4vh, 48px) clamp(20px, 5vw, 64px);
    gap: clamp(12px, 2vh, 24px);
    transition: opacity 2s ease; will-change: opacity;
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
  .standby .brand-ready {
    margin: 0; display: inline-flex; align-items: center; gap: 8px;
    font-size: clamp(13px, 1.2vw, 16px); letter-spacing: .14em;
    text-transform: uppercase; color: #35c777; font-weight: 700;
  }
  .standby .brand-ready .status-dot {
    background: #35c777; box-shadow: 0 0 0 3px rgba(53,199,119,.16);
  }
  .standby-dlna {
    display: inline-flex; align-items: center; gap: 8px; width: fit-content;
    padding: 7px 11px; border: 1px solid rgba(255,255,255,.14);
    border-radius: 999px; background: rgba(5,9,16,.24);
    color: rgba(241,246,255,.82); backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
    font-size: clamp(12px, 1.05vw, 15px); font-weight: 600;
  }
  .standby-dlna .status-dot { background: #35c777; box-shadow: 0 0 0 3px rgba(53,199,119,.16); }
  .standby-dlna.unavailable { color: #ffc9c5; }
  .standby-dlna.unavailable .status-dot { background: #ff665d; box-shadow: 0 0 0 3px rgba(255,102,93,.16); }
  .fs-exit {
    font-size: 13px; font-weight: 600; min-width: 0; white-space: nowrap;
    background: rgba(255,255,255,.08); border-color: rgba(255,255,255,.18);
    color: #f4f7fb; backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  }
  .fs-exit:hover { background: rgba(255,255,255,.14); border-color: rgba(255,255,255,.28); }
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
    border-radius: 14px; background: #fff; padding: 8px;
    box-shadow: 0 12px 40px rgba(0,0,0,.28);
  }
  .photo-credit {
    margin: 0; font-size: 12px; letter-spacing: .02em;
    color: rgba(232,240,255,.48); text-shadow: 0 1px 8px rgba(0,0,0,.4);
  }

  body.mode-standby .compact { display: none; }
  body.mode-standby .standby { display: block; }
  .standby.hud-hidden .standby-inner { opacity: 0; pointer-events: none; }
  .standby.is-sleeping .standby-blackout { opacity: 1; }
  .standby.is-sleeping .standby-bg { animation-play-state: paused; }
  .standby.is-waking .standby-blackout { transition-duration: 1s; }

  @media (max-width: 700px), (max-height: 500px) {
    .standby-top { flex-wrap: wrap; }
    .standby-qr { width: 96px; height: 96px; }
  }
  /* Narrow window / browser-tab fallback: stack brand above connect. */
  @media (max-width: 760px) {
    .compact { padding: 16px; height: auto; min-height: 100vh; }
    .compact-columns { flex-direction: column; align-items: center; text-align: center; gap: 0; }
    .compact-side {
      flex: none; width: 100%%; max-width: 420px; align-items: center;
      padding-right: 0; padding-bottom: 16px;
      border-right: none; border-bottom: 1px solid var(--divider);
    }
    .brand-core { padding: 8px 0; }
    .compact-main {
      width: 100%%; max-width: 420px; text-align: center;
      padding-left: 0; padding-top: 16px;
    }
    .connect-card { flex: 0 0 auto; }
    .connect { flex-direction: column; align-items: center; }
    .connect-qr-col { width: 100%%; }
    .connect-info { align-items: center; }
    .compact .intro { max-width: 36ch; text-align: center; }
    .url-row { justify-content: center; }
    .pair-row { justify-content: center; }
    .network-help summary, .network-help[open] summary { text-align: center; }
    .footer-row { flex-direction: column; gap: 8px; margin-top: 16px; }
  }
  @media (prefers-reduced-motion: reduce) {
    .standby-bg { animation: none !important; transition-duration: .01ms; }
    .standby-blackout, .standby-inner { transition-duration: .01ms; }
    button { transition: none; }
    .footer-spinner { animation: none; border-top-color: var(--muted); opacity: .7; }
    .pair-card.is-visible { animation: none; }
  }
</style></head>
<body class="mode-compact">
  <div class="compact">
    <div class="compact-columns">
      <div class="compact-side">
        <div class="brand-core">
          <img class="brand-logo" src="/static/pwa-icon-512.png" alt="TinyPlay" width="108" height="108">
        </div>
      </div>
      <div class="compact-main">
        <section class="connect-card">
          <div class="connect">
            <div class="connect-qr-col">
              <div class="compact-qr-stage" id="qr-stage">
                <img class="qr" id="qr-image" src="/desktop/qr.png" alt="QR" width="188" height="188">
                <section class="pair-card is-locked" id="pair-locked" role="alert">
                  <p class="pair-card-title">%s</p>
                  <p class="pair-card-hint">%s</p>
                  <div class="pair-actions">
                    <button type="button" class="pair-primary" id="pair-refresh">%s</button>
                  </div>
                </section>
                <section class="pair-card is-consent" id="pair-consent" aria-live="polite">
                  <p class="pair-card-title">%s</p>
                  <div class="pair-code" id="pair-code">····</div>
                  <p class="pair-card-hint">%s</p>
                  <div class="pair-actions">
                    <button type="button" class="pair-primary" id="pair-approve">%s</button>
                    <button type="button" class="pair-secondary" id="pair-deny">%s</button>
                  </div>
                </section>
              </div>
              <div class="fullscreen-action">
                <button type="button" class="fs-enter" id="fs-enter" aria-pressed="false">%s</button>
              </div>
            </div>
            <div class="connect-info">
              <p class="intro">%s</p>
              <div class="url-row">
                <code id="url-compact">%s</code>
                <span class="status-pill available compact-ready" role="status"><span class="status-dot"></span>%s</span>
              </div>
              <div class="pair-row" id="pair-devices" hidden>
                <span id="pair-devices-text" data-template="%s"></span>
                <button type="button" class="pair-link" id="pair-unpair">%s</button>
              </div>
              <div class="pair-row" id="pair-unpair-confirm" hidden>
                <span>%s</span>
                <button type="button" class="pair-link danger" id="pair-unpair-yes">%s</button>
                <button type="button" class="pair-link" id="pair-unpair-no">%s</button>
              </div>
              %s
            </div>
          </div>
        </section>
        <div class="aux-list">
          %s
          %s
        </div>
        %s
        %s
      </div>
    </div>
  </div>

  <div class="standby" id="standby" aria-hidden="true">
    <div class="standby-bg" id="standby-bg-a" role="presentation"></div>
    <div class="standby-bg" id="standby-bg-b" role="presentation"></div>
    <div class="standby-scrim" role="presentation"></div>
    <div class="standby-blackout" id="standby-blackout" role="presentation"></div>
    <div class="standby-inner">
      <div class="standby-top">
        <div class="brand">
          <div class="brand-heading">
            <h1 class="brand-name">TinyPlay</h1>
            <p class="brand-ready"><span class="status-dot" aria-hidden="true"></span>%s</p>
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
        <p class="photo-credit" id="photo-credit">%s</p>
      </div>
    </div>
  </div>

  <script>
    (() => {
      const body = document.body;
      const standby = document.getElementById('standby');
      const blackout = document.getElementById('standby-blackout');
      const enterBtn = document.getElementById('fs-enter');
      const exitBtn = document.getElementById('fs-exit');
      const clockEl = document.getElementById('clock');
      const dateEl = document.getElementById('clock-date');
      const photoCredit = document.getElementById('photo-credit');
      const lang = document.documentElement.lang || 'en';
      const backgroundLayers = [
        document.getElementById('standby-bg-a'),
        document.getElementById('standby-bg-b'),
      ];
      const backgrounds = [
        { src: '/desktop/background.jpg?asset=carina', credit: 'NASA, ESA, CSA, STScI' },
        { src: '/desktop/background.jpg?asset=ngc6000', credit: 'ESA/Hubble & NASA, A. Filippenko; acknowledgment: M. H. Özsaraç' },
        { src: '/desktop/background.jpg?asset=earthrise', credit: 'NASA / Goddard Space Flight Center / Arizona State University' },
      ];
      const IMAGE_HOLD_MS = 240000;
      const HUD_INITIAL_MS = 120000;
      const HUD_WAKE_MS = 20000;
      const IDLE_SLEEP_MS = 1800000;
      let wantStandby = false;
      let nativeBridge = false;
      let activeLayer = 0;
      let backgroundIndex = -1;
      let backgroundTimer;
      let hudTimer;
      let sleepTimer;
      let clockTimer;
      let lastPointer;

      const clearTimer = (timer) => {
        if (timer) clearTimeout(timer);
      };

      const hideHUD = () => standby.classList.add('hud-hidden');
      const showHUD = (duration) => {
        standby.classList.remove('hud-hidden');
        clearTimer(hudTimer);
        hudTimer = setTimeout(hideHUD, duration);
      };

      const chooseBackground = () => {
        if (backgrounds.length === 1) return 0;
        let next = Math.floor(Math.random() * backgrounds.length);
        while (next === backgroundIndex) next = Math.floor(Math.random() * backgrounds.length);
        return next;
      };

      const setBackground = () => {
        const nextIndex = chooseBackground();
        const nextLayer = backgroundLayers[1 - activeLayer];
        const previousLayer = backgroundLayers[activeLayer];
        const background = backgrounds[nextIndex];
        backgroundIndex = nextIndex;
        photoCredit.textContent = background.credit;
        nextLayer.className = 'standby-bg';
        nextLayer.style.backgroundImage = 'url("' + background.src + '")';
        // Restart one of the three gentle pan paths for every four-minute still.
        void nextLayer.offsetWidth;
        nextLayer.classList.add('motion-' + (nextIndex %% 3), 'is-active');
        previousLayer.classList.remove('is-active');
        activeLayer = 1 - activeLayer;
      };

      const scheduleBackground = () => {
        clearTimer(backgroundTimer);
        backgroundTimer = setTimeout(() => {
          if (wantStandby && !standby.classList.contains('is-sleeping')) {
            setBackground();
            scheduleBackground();
          }
        }, IMAGE_HOLD_MS);
      };

      const enterSleep = () => {
        if (!wantStandby) return;
        hideHUD();
        clearTimer(hudTimer);
        clearTimer(backgroundTimer);
        clearInterval(clockTimer);
        clockTimer = undefined;
        standby.classList.add('is-sleeping');
      };

      const armSleep = () => {
        clearTimer(sleepTimer);
        sleepTimer = setTimeout(enterSleep, IDLE_SLEEP_MS);
      };

      const wakeStandby = (duration = HUD_WAKE_MS) => {
        if (!wantStandby) return;
        if (standby.classList.contains('is-sleeping')) {
          standby.classList.add('is-waking');
          void blackout.offsetWidth;
          standby.classList.remove('is-sleeping');
          setTimeout(() => standby.classList.remove('is-waking'), 1000);
          scheduleBackground();
          startClock();
        }
        showHUD(duration);
        armSleep();
      };

      const startStandby = () => {
        setBackground();
        scheduleBackground();
        startClock();
        showHUD(HUD_INITIAL_MS);
        armSleep();
      };

      const stopStandby = () => {
        clearTimer(backgroundTimer);
        clearTimer(hudTimer);
        clearTimer(sleepTimer);
        clearInterval(clockTimer);
        clockTimer = undefined;
        standby.classList.remove('hud-hidden', 'is-sleeping', 'is-waking');
      };

      const setLayout = (standbyOn) => {
        body.classList.toggle('mode-standby', standbyOn);
        body.classList.toggle('mode-compact', !standbyOn);
        standby.setAttribute('aria-hidden', standbyOn ? 'false' : 'true');
        enterBtn.setAttribute('aria-pressed', standbyOn ? 'true' : 'false');
        if (standbyOn) startStandby();
        else stopStandby();
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
        if (!wantStandby) return;
        if (e.key === 'Escape' && wantStandby) {
          e.preventDefault();
          if (standby.classList.contains('hud-hidden') || standby.classList.contains('is-sleeping')) {
            wakeStandby();
            return;
          }
          setStandby(false);
          return;
        }
        wakeStandby();
      });

      const pointerActivity = (e) => {
        if (!wantStandby) return;
        if (e.type === 'pointermove') {
          const point = { x: e.clientX, y: e.clientY };
          const moved = !lastPointer || Math.hypot(point.x - lastPointer.x, point.y - lastPointer.y) >= 24;
          lastPointer = point;
          if (!moved) return;
        }
        wakeStandby();
      };
      document.addEventListener('pointermove', pointerActivity, { passive: true });
      document.addEventListener('pointerdown', pointerActivity, { passive: true });
      document.addEventListener('wheel', pointerActivity, { passive: true });
      document.addEventListener('touchstart', pointerActivity, { passive: true });

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
      const startClock = () => {
        if (clockTimer) return;
        tick();
        clockTimer = setInterval(tick, 1000);
      };

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

      /* —— Pairing: consent prompts and QR lockout —— */
      const qrStage = document.getElementById('qr-stage');
      const qrImage = document.getElementById('qr-image');
      const lockedCard = document.getElementById('pair-locked');
      const consentCard = document.getElementById('pair-consent');
      const codeEl = document.getElementById('pair-code');
      const devicesRow = document.getElementById('pair-devices');
      const devicesText = document.getElementById('pair-devices-text');
      const unpairConfirm = document.getElementById('pair-unpair-confirm');
      let pendingNonce = '';
      let busy = false;

      const reloadQR = () => {
        // Cache-bust so a refreshed secret shows up as a new image.
        qrImage.src = '/desktop/qr.png?t=' + Date.now();
      };

      const pairingPost = async (path, body) => {
        if (busy) return;
        busy = true;
        try {
          await fetch(path, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body || {}),
          });
        } catch (_) {
        } finally {
          busy = false;
        }
        pollPairing();
      };

      const paintPairing = (state) => {
        const pending = state.pending || null;
        const locked = state.locked === true && !pending;
        pendingNonce = pending ? pending.nonce : '';
        if (pending) {
          codeEl.textContent = pending.code;
          codeEl.setAttribute('aria-label', pending.code);
        } else {
          codeEl.removeAttribute('aria-label');
        }
        consentCard.classList.toggle('is-visible', !!pending);
        lockedCard.classList.toggle('is-visible', locked);
        // Only the QR plate swaps; keep the rest of the connect card stable.
        qrStage.classList.toggle('has-card', !!pending || locked);
        qrStage.setAttribute('aria-busy', (!!pending || locked) ? 'true' : 'false');

        const count = Number(state.device_count || 0);
        devicesRow.hidden = count === 0 || !unpairConfirm.hidden;
        devicesText.textContent = devicesText.dataset.template.replace('{n}', String(count));
        if (count === 0) unpairConfirm.hidden = true;
      };

      const pollPairing = async () => {
        try {
          const state = await fetch('/desktop/pairing', { cache: 'no-store' }).then(r => r.ok ? r.json() : null);
          if (state) paintPairing(state);
        } catch (_) {}
      };

      document.getElementById('pair-refresh').addEventListener('click', async () => {
        await pairingPost('/desktop/pairing/refresh');
        reloadQR();
      });
      document.getElementById('pair-approve').addEventListener('click', () => {
        // Send the nonce whose code is on screen, so the click can only ever
        // approve the request the person actually compared.
        if (pendingNonce) pairingPost('/desktop/pairing/approve', { nonce: pendingNonce });
      });
      document.getElementById('pair-deny').addEventListener('click', () => {
        if (pendingNonce) pairingPost('/desktop/pairing/deny', { nonce: pendingNonce });
      });
      document.getElementById('pair-unpair').addEventListener('click', () => {
        // Inline confirmation instead of window.confirm(): native WebViews do
        // not always present JS dialogs, and a silently-dropped confirm would
        // turn into a silently-ignored button.
        devicesRow.hidden = true;
        unpairConfirm.hidden = false;
      });
      document.getElementById('pair-unpair-no').addEventListener('click', () => {
        unpairConfirm.hidden = true;
        devicesRow.hidden = false;
      });
      document.getElementById('pair-unpair-yes').addEventListener('click', async () => {
        unpairConfirm.hidden = true;
        await pairingPost('/desktop/pairing/unpair-all');
        reloadQR();
      });

      pollPairing();
      setInterval(pollPairing, 2000);

      const status = document.getElementById('dlna-status');
      let enabled = status.childElementCount > 0;
      setInterval(async () => {
        try {
          const settings = await fetch('/api/settings', { cache: 'no-store' }).then(r => r.json());
          const next = settings.dlna_receiver_enabled !== false;
          if (next !== enabled) location.reload();
        } catch (_) {}
      }, 3000);

      /* —— Aux rows: DLNA toggle, desktop-input authorize, footer actions —— */
      document.getElementById('dlna-toggle')?.addEventListener('click', async (e) => {
        const btn = e.currentTarget;
        const next = btn.getAttribute('aria-checked') !== 'true';
        btn.disabled = true;
        try {
          await fetch('/api/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ dlna_receiver_enabled: next }),
          });
        } catch (_) {}
        location.reload();
      });
      document.getElementById('input-authorize')?.addEventListener('click', async (e) => {
        e.currentTarget.disabled = true;
        try {
          await fetch('/api/system/input/action', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'request_permission' }),
          });
        } catch (_) {}
        // Keep the page up so the restart hint/button stay visible while the
        // person flips the Accessibility toggle; only re-enable the button.
        setTimeout(() => { e.currentTarget.disabled = false; }, 900);
      });
      document.getElementById('input-restart')?.addEventListener('click', async (e) => {
        e.currentTarget.disabled = true;
        // Prefer the shell bridge (immediate). Fall back to the input command
        // queue so a plain browser tab on loopback still reaches the shell.
        if (typeof window.tinyplayRestart === 'function') {
          window.tinyplayRestart();
          return;
        }
        const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.tinyplayRestart;
        if (handler) {
          handler.postMessage(null);
          return;
        }
        try {
          await fetch('/api/system/input/action', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'restart_app' }),
          });
        } catch (_) {
          e.currentTarget.disabled = false;
        }
      });
      document.getElementById('btn-open-logs')?.addEventListener('click', () => {
        fetch('/desktop/open-logs').catch(() => {});
      });
      const nativeCall = (name) => {
        if (typeof window[name] === 'function') { return window[name](); }
        const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers[name];
        if (handler) { handler.postMessage(null); return true; }
        return false;
      };
      // Version label opens the native About dialog (replaces a separate About link).
      document.getElementById('btn-about')?.addEventListener('click', () => { nativeCall('tinyplayShowAbout'); });

      /* Check for Updates: disable + spinner until the native shell finishes
         (Windows Bind awaits; macOS posts __tinyplayUpdateCheckDone). */
      const updateBtn = document.getElementById('btn-check-updates');
      let updateCheckTimer;
      const setUpdateChecking = (on) => {
        if (!updateBtn) return;
        updateBtn.disabled = !!on;
        updateBtn.classList.toggle('is-checking', !!on);
        updateBtn.setAttribute('aria-busy', on ? 'true' : 'false');
        if (!on && updateCheckTimer) {
          clearTimeout(updateCheckTimer);
          updateCheckTimer = undefined;
        }
      };
      window.__tinyplayUpdateCheckDone = () => setUpdateChecking(false);
      updateBtn?.addEventListener('click', async () => {
        if (!updateBtn || updateBtn.disabled) return;
        setUpdateChecking(true);
        // Safety net if a shell never signals completion (e.g. plain browser tab).
        updateCheckTimer = setTimeout(() => setUpdateChecking(false), 20000);
        try {
          if (typeof window.tinyplayCheckForUpdates === 'function') {
            await window.tinyplayCheckForUpdates();
            setUpdateChecking(false);
            return;
          }
          if (!nativeCall('tinyplayCheckForUpdates')) {
            setUpdateChecking(false);
          }
          // macOS: shell calls __tinyplayUpdateCheckDone when the check ends.
        } catch (_) {
          setUpdateChecking(false);
        }
      });
    })();
  </script>
</body></html>`,
		lang,
		i18n.T(lang, "pair_locked_title"),
		i18n.T(lang, "pair_locked_body"),
		i18n.T(lang, "pair_refresh"),
		i18n.T(lang, "pair_consent_title"),
		i18n.T(lang, "pair_consent_body"),
		i18n.T(lang, "pair_approve"),
		i18n.T(lang, "pair_deny"),
		i18n.T(lang, "desktop_fullscreen"),
		i18n.T(lang, "desktop_intro"),
		url,
		i18n.T(lang, "desktop_standby_ready"),
		i18n.T(lang, "pair_devices_template"),
		i18n.T(lang, "pair_unpair_all"),
		i18n.T(lang, "pair_unpair_warning"),
		i18n.T(lang, "pair_unpair_yes"),
		i18n.T(lang, "pair_unpair_cancel"),
		desktopNetworkHelp(denied, lang, help),
		dlnaSection,
		inputSection,
		notices,
		footerSection,
		i18n.T(lang, "desktop_standby_ready"),
		standbyDLNA,
		i18n.T(lang, "desktop_exit_fullscreen"),
		i18n.T(lang, "desktop_photo_credit"),
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// desktopBackground serves one bundled HTPC standby backdrop. Immutable cache
// is safe because each asset is compiled into the binary and only changes with
// a new build. The empty asset remains the original Carina image for backward
// compatibility with older desktop shells.
func (s *Server) desktopBackground(w http.ResponseWriter, r *http.Request) {
	assetName := r.URL.Query().Get("asset")
	if assetName == "" {
		assetName = "carina"
	}
	asset, ok := desktopBackgroundAssets[assetName]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(asset.image)))
	w.Write(asset.image)
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

// desktopDLNASection is the "DLNA 接收器" row in the compact window's aux
// list: a label, the same status pill desktopDLNAStatus renders (which
// already embeds dlna.FriendlyName() so the user knows which device to pick
// when casting), and a toggle that moves the on/off switch out of the tray
// menu. #dlna-status keeps its id so the page's existing "settings changed
// elsewhere, reload" poll keeps working unchanged.
func (s *Server) desktopDLNASection(lang string) string {
	enabled := config.Load().DLNAReceiverEnabled
	toggleClass := ""
	if enabled {
		toggleClass = " on"
	}
	return fmt.Sprintf(`<div class="aux-row">
            <span class="aux-label">%s</span>
            <span class="aux-spacer" aria-hidden="true"></span>
            <span id="dlna-status">%s</span>
            <button type="button" class="switch%s" id="dlna-toggle" role="switch" aria-checked="%t" aria-label="%s"></button>
          </div>`,
		i18n.T(lang, "dlna_receiver"), s.desktopDLNAStatus(lang), toggleClass, enabled, i18n.T(lang, "dlna_receiver"))
}

// desktopInputSection is the "模拟键鼠" row: a temporary air-mouse/keyboard
// bridge the phone can drive (see internal/desktopinput). It renders nothing
// at all when no native shell has ever reported in — the same headless-dev
// guard app.js uses for its own copy of this feature — so the row simply
// never appears until the next reload once a shell is attached. On macOS,
// where AXIsProcessTrusted() gates real injection, an unauthorized state gets
// "Authorize" + "Restart" plus a short hint: macOS often keeps the current
// process untrusted until relaunch. Windows has no such gate (SendInput needs
// no OS consent) and is always reported ready, so it never shows the buttons.
func desktopInputSection(lang string) string {
	snap := desktopinput.Default.Snapshot()
	available := snap.Ready || snap.PermissionRequired || snap.PermissionGranted
	if !available {
		return ""
	}
	granted := !snap.PermissionRequired || snap.PermissionGranted
	status, label, actions, hint := "unavailable", i18n.T(lang, "desktop_input_unauthorized"),
		fmt.Sprintf(`<span class="aux-actions"><button type="button" class="aux-action" id="input-authorize">%s</button><button type="button" class="aux-action" id="input-restart">%s</button></span>`,
			i18n.T(lang, "desktop_input_authorize"), i18n.T(lang, "desktop_input_restart")),
		fmt.Sprintf(`<p class="aux-hint">%s</p>`, i18n.T(lang, "desktop_input_restart_hint"))
	if granted {
		status, label, actions, hint = "available", i18n.T(lang, "desktop_input_ready"), "", ""
	}
	return fmt.Sprintf(`<div class="aux-row">
            <span class="aux-label">%s</span>
            <span class="aux-spacer" aria-hidden="true"></span>
            <span class="status-pill %s" role="status"><span class="status-dot"></span>%s</span>
            %s
          </div>%s`, i18n.T(lang, "desktop_input_label"), status, label, actions, hint)
}

// desktopFooterSection groups the low-frequency actions that used to live
// only in the tray menu. The version label doubles as the About entry
// (click → native about dialog). Check-for-updates and open-logs keep the
// same Bind/postMessage bridges as the full-screen toggle.
func desktopFooterSection(lang, version string) string {
	// Outside a packaged shell there is no ?version=; keep a clickable About
	// affordance so the dialog is still reachable during `go run` development.
	aboutLabel := i18n.T(lang, "about")
	if version != "" {
		aboutLabel = "v" + version
	}
	return fmt.Sprintf(`<div class="footer-row">
          <div class="footer-group"><button type="button" class="footer-link" id="btn-about" title="%s">%s</button><button type="button" class="footer-link" id="btn-check-updates" aria-busy="false">%s<span class="footer-spinner" aria-hidden="true"></span></button></div>
          <div class="footer-group"><button type="button" class="footer-link" id="btn-open-logs">%s</button></div>
        </div>`, i18n.T(lang, "about"), aboutLabel, i18n.T(lang, "check_updates"), i18n.T(lang, "open_logs"))
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

// desktopQR renders the pairing URL as a PNG QR code.
//
// The image carries the pairing secret, so it is loopback-only: it must be
// readable by the native window on this computer and by nobody on the network.
// The secret rides in the URL fragment, which browsers never send to a server —
// it stays out of access logs and Referer headers, and the frontend strips it
// from the address bar as soon as it has been exchanged for a token.
func (s *Server) desktopQR(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	png, err := qrcode.Encode(s.pairingURL(), qrcode.Medium, 480)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// pairingURL is what the QR code encodes. While pairing is locked the secret is
// left out entirely, so a photo taken during a lockout cannot be used later.
func (s *Server) pairingURL() string {
	if auth.Default.Locked() {
		return s.phoneURL()
	}
	return s.phoneURL() + "/#p=" + auth.Default.Secret()
}
