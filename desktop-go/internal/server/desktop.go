package server

import (
	"fmt"
	"html"
	"net/http"

	"github.com/skip2/go-qrcode"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
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
	mpv := player.DetectMPV()
	mpvStatus := i18n.T(lang, "mpv_unavailable")
	if mpv.Available {
		labels := map[string]string{
			"custom":  i18n.T(lang, "mpv_source_custom"),
			"bundled": i18n.T(lang, "mpv_source_bundled"),
			"system":  i18n.T(lang, "mpv_source_system"),
		}
		mpvStatus = fmt.Sprintf("mpv: %s (%s)", labels[mpv.Source], html.EscapeString(mpv.Path))
	}
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>TinyPlay</title>
<link rel="icon" href="/static/favicon.png">
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
	.runtime { font-size: 12px; max-width: 320px; overflow-wrap: anywhere; opacity: .65; }
</style></head>
<body>
  <h1>TinyPlay</h1>
  <p>%s</p>
  <img src="/desktop/qr.png" alt="QR">
  <code>%s</code>
	<div class="runtime">%s</div>
</body></html>`, lang, i18n.T(lang, "desktop_intro"), url, mpvStatus)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
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
