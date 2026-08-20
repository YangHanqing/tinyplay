package server

import (
	"fmt"
	"net/http"
)

// desktopMonitorHintReport lets the native macOS shell push the NSScreen
// index its own window currently sits on (Go and the shell are separate
// processes there, unlike Windows where the WebView2 host and mpv share one
// process and can query the display live). Player.Play then opens a fresh
// mpv with --fs-screen=<index>, so playback follows whichever monitor the
// user last dragged the shell window to. Windows never calls this;
// player.Player.MonitorHint queries the OS directly instead.
func (s *Server) desktopMonitorHintReport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	var body struct {
		Screen int `json:"screen"`
	}
	if !decode(r, &body) || body.Screen < 0 {
		invalidBody(w, r)
		return
	}
	s.player.SetMonitorHint(fmt.Sprintf("--fs-screen=%d", body.Screen))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
