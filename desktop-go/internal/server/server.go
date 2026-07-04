// Package server is the Go port of the FastAPI app (app/main.py + routes). It
// exposes the same REST surface and serves the embedded frontend.
package server

import (
	"io/fs"
	"net/http"

	"tvremote/internal/player"
	"tvremote/internal/webui"
)

// Server holds the shared dependencies for the HTTP handlers.
type Server struct {
	player       *player.Player
	webFS        fs.FS
	port         int  // the actual bound port (may differ from config if it was taken)
	preferNative bool // true → use AVPlayer on macOS; toggled via /api/desktop/engine
}

// New builds the server. The player's reporters should already be wired.
func New(p *player.Player) *Server {
	return &Server{player: p, webFS: webui.FS()}
}

// SetPort records the port the HTTP server actually bound to, so the QR / intro
// page advertise the right address even when we fell back off a busy port.
func (s *Server) SetPort(port int) { s.port = port }

// Handler returns the full router with request logging applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ── Server management ──
	mux.HandleFunc("GET /api/servers", s.listServers)
	mux.HandleFunc("POST /api/servers", s.createServer)
	mux.HandleFunc("PUT /api/servers/{id}", s.editServer)
	mux.HandleFunc("DELETE /api/servers/{id}", s.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/activate", s.activateServer)
	mux.HandleFunc("PUT /api/servers/{id}/host", s.switchHost)
	mux.HandleFunc("POST /api/servers/{id}/login", s.loginServer)

	// ── App settings ──
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.updateSettings)

	// ── Player ──
	mux.HandleFunc("GET /api/player/state", s.playerState)
	mux.HandleFunc("POST /api/player/play", s.playItem)
	mux.HandleFunc("POST /api/player/command", s.playerCommand)
	mux.HandleFunc("POST /api/player/stop", s.playerStop)
	mux.HandleFunc("GET /api/player/props", s.playerProps)

	// ── Emby ──
	mux.HandleFunc("GET /api/emby/libraries", s.embyLibraries)
	mux.HandleFunc("GET /api/emby/resume", s.embyResume)
	mux.HandleFunc("GET /api/emby/items", s.embyItems)
	mux.HandleFunc("GET /api/emby/items/{item_id}", s.embyItemDetail)
	mux.HandleFunc("GET /api/emby/episodes", s.embyEpisodes)
	mux.HandleFunc("GET /api/emby/image/{item_id}", s.embyImage)

	// ── Desktop intro + QR (used by the native shells) ──
	mux.HandleFunc("GET /desktop", s.desktopPage)
	mux.HandleFunc("GET /desktop/qr.png", s.desktopQR)
	mux.HandleFunc("GET /desktop/open-logs", s.openLogs)

	// ── Player engine picker (macOS AVPlayer vs mpv) ──
	mux.HandleFunc("GET /api/desktop/engine", s.engineGet)
	mux.HandleFunc("POST /api/desktop/engine", s.engineSet)

	// ── Frontend (embedded) ──
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.webFS))))
	mux.HandleFunc("GET /manifest.webmanifest", s.webManifest)
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /", s.index)

	return withLogging(mux)
}
