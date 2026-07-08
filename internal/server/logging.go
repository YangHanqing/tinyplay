package server

import (
	"log"
	"net/http"
	"strings"
	"time"

	"tvremote/internal/i18n"
)

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// withLogging mirrors app/main.py's _RequestLogger: log API calls with a short
// Chinese action label, skipping the props heartbeat and image fetches.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		skip := path == "/api/player/props" ||
			!strings.HasPrefix(path, "/api/") ||
			strings.Contains(path, "/api/emby/image/")
		if skip {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		t0 := time.Now()
		next.ServeHTTP(rec, r)
		ms := time.Since(t0).Milliseconds()
		mark := "✓"
		if rec.status >= 400 {
			mark = "✗"
		}
		log.Printf("  %s [%-6s] %-14s  %5dms  HTTP %d", mark, r.Method, actionLabel(i18n.RequestLang(r), r.Method, path), ms, rec.status)
	})
}

func actionLabel(lang, method, path string) string {
	segs := strings.Split(path, "/")
	if len(segs) < 3 {
		return path
	}
	ns := segs[2]
	sub := ""
	if len(segs) > 3 {
		sub = segs[3]
	}
	tail := ""
	if len(segs) > 4 {
		tail = segs[4]
	}

	switch ns {
	case "player":
		m := map[string]string{
			"state": i18n.T(lang, "log_player_state"), "play": i18n.T(lang, "log_play"), "command": i18n.T(lang, "log_player_command"),
			"stop": i18n.T(lang, "log_stop"), "props": i18n.T(lang, "log_props"),
		}
		if v, ok := m[sub]; ok {
			return v
		}
	case "emby":
		m := map[string]string{
			"libraries": i18n.T(lang, "log_libraries"), "resume": i18n.T(lang, "log_resume"),
			"items": i18n.T(lang, "log_items"), "episodes": i18n.T(lang, "log_episodes"),
		}
		if v, ok := m[sub]; ok {
			return v
		}
		return "Emby " + sub
	case "servers":
		if sub == "" {
			if method == "GET" {
				return i18n.T(lang, "log_servers_list")
			}
			return i18n.T(lang, "log_server_add")
		}
		switch tail {
		case "activate":
			return i18n.T(lang, "log_server_activate")
		case "host":
			return i18n.T(lang, "log_server_host")
		case "login":
			return i18n.T(lang, "log_server_login")
		case "":
			if method == "PUT" || method == "PATCH" {
				return i18n.T(lang, "log_server_edit")
			}
			if method == "DELETE" {
				return i18n.T(lang, "log_server_delete")
			}
			return i18n.T(lang, "log_server_get")
		}
	case "settings":
		return i18n.T(lang, "log_settings")
	}
	return path
}
