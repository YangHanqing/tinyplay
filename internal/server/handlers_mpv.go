package server

import (
	"net/http"
	"strings"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

// ── Advanced settings: custom mpv executable (loopback only) ──
//
// Both native shells' tray/menu-bar "高级设置 → 自定义 mpv 播放器…" flow talks to
// these two routes over 127.0.0.1, the same way the pairing/website/input
// desktop routes do (see withGuard in server.go). This is a desktop-shell
// control, not a phone-facing setting, so it has no /api/ counterpart.

// mpvStatus reports the state desktopMpvStatus needs to render itself:
// what mpv is actually resolving to right now, and whether a configured
// custom path is the reason (or has gone stale).
func (s *Server) mpvStatus(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	info := player.DetectMPV()
	writeJSON(w, http.StatusOK, map[string]any{
		"path":              info.Path,
		"source":            info.Source,
		"available":         info.Available,
		"custom_configured": info.CustomConfigured,
		"custom_valid":      info.CustomConfigured && !info.CustomInvalid,
		// bundled_unusable lets both shells' tray tooltip stop claiming "using
		// the bundled mpv player" on a system that cannot run it.
		"bundled_unusable": info.BundledUnusable,
	})
}

// mpvSetCustom persists (or clears) the user's custom mpv path. A non-empty
// path must validate as a real mpv binary first — see player.ValidateMPV —
// so a typo or a path to the wrong program is rejected before it ever
// reaches config.json. On success, a currently-running mpv process is
// stopped (mirroring the existing website/mpv mutual-exclusion Stop() calls
// elsewhere in this package) so the new path takes effect from the next
// playback rather than mid-title.
func (s *Server) mpvSetCustom(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	path := strings.TrimSpace(body.Path)
	if path != "" {
		if err := player.ValidateMPV(path); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"detail": err.Error()})
			return
		}
	}
	config.SetMpvExe(path)
	if state, ok := s.player.State()["running"].(bool); ok && state {
		s.invalidatePlay()
		_ = s.player.Stop()
	}
	info := player.DetectMPV()
	writeJSON(w, http.StatusOK, map[string]any{
		"path":              info.Path,
		"source":            info.Source,
		"available":         info.Available,
		"custom_configured": info.CustomConfigured,
		"custom_valid":      info.CustomConfigured && !info.CustomInvalid,
		// bundled_unusable lets both shells' tray tooltip stop claiming "using
		// the bundled mpv player" on a system that cannot run it.
		"bundled_unusable": info.BundledUnusable,
	})
}
