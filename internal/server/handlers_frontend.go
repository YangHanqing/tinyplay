package server

import (
	"net/http"

	"tvremote/internal/i18n"
)

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.frontendAsset(w, r, "index.html", "text/html; charset=utf-8")
}

func (s *Server) webManifest(w http.ResponseWriter, r *http.Request) {
	s.frontendAsset(w, r, "manifest.webmanifest", "application/manifest+json; charset=utf-8")
}

func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	s.frontendAsset(w, r, "sw.js", "application/javascript; charset=utf-8")
}

func (s *Server) frontendAsset(w http.ResponseWriter, r *http.Request, name, contentType string) {
	data, err := readWeb(s, name)
	if err != nil {
		http.Error(w, i18n.Request(r, "frontend_not_built"), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
