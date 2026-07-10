package server

import (
	"fmt"
	"net/http"
	"runtime"

	"github.com/skip2/go-qrcode"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
)

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
// SwiftUI shell may render this natively instead; the Windows shell loads it in
// a WebView2.
func (s *Server) desktopPage(w http.ResponseWriter, r *http.Request) {
	url := s.phoneURL()
	lang := i18n.RequestLang(r)
	help := i18n.T(lang, desktopNetworkHelpKey())
	denied := runtime.GOOS == "darwin" && r.URL.Query().Get("local_network") == "denied"
	notice := ""
	if denied {
		notice = fmt.Sprintf(`<section class="notice error" role="alert"><strong>%s</strong><span>%s</span></section>`,
			i18n.T(lang, "desktop_network_denied_title"), i18n.T(lang, "desktop_network_denied_body"))
	}
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>TinyPlay</title>
<link rel="icon" href="/static/favicon.ico" sizes="any">
<style>
  :root { color-scheme: light dark; }
  body { font-family: -apple-system, "Segoe UI", system-ui, sans-serif;
         margin: 0; min-height: 100vh; display: flex; flex-direction: column;
         align-items: center; justify-content: center; gap: 18px; text-align: center; }
  h1 { font-size: 22px; margin: 0; }
  p  { margin: 0; opacity: .7; max-width: 320px; line-height: 1.5; }
  img { width: 240px; height: 240px; border-radius: 12px; background: #fff; padding: 12px; }
  code { font-size: 15px; padding: 4px 10px; border-radius: 6px;
         background: rgba(127,127,127,.15); }
  .notice, details { width: min(320px, calc(100vw - 40px)); box-sizing: border-box;
                     text-align: left; border-radius: 10px; padding: 12px 14px;
                     background: rgba(127,127,127,.12); font-size: 13px; line-height: 1.5; }
  .notice { display: grid; gap: 5px; }
  .notice.error { background: rgba(218,54,51,.16); color: #a61b1b; }
  .notice strong { font-size: 14px; }
  details { padding: 0; overflow: hidden; }
  summary { cursor: pointer; padding: 12px 14px; font-weight: 600; }
  details p { max-width: none; padding: 0 14px 13px; font-size: 13px; opacity: .76; }
</style></head>
<body>
  <h1>TinyPlay</h1>
  <p>%s</p>
  <img src="/desktop/qr.png" alt="QR">
  <code>%s</code>
  %s
  <details><summary>%s</summary><p>%s</p></details>
</body></html>`, lang, i18n.T(lang, "desktop_intro"), url, notice, i18n.T(lang, "desktop_network_help_title"), help)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
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
