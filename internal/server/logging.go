package server

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tvremote/internal/i18n"
)

// withGuard blocks cross-site requests to the JSON API. The remote has no
// per-user auth by design — it is a LAN control surface — so the one thing that
// must be prevented is a web page in the user's browser silently driving the
// API (most dangerously POST /api/player/command). Two cheap, standard checks
// stop that without inconveniencing the phone frontend:
//
//   - A state-changing /api request must carry Content-Type: application/json.
//     A cross-origin fetch that sets this header triggers a CORS preflight the
//     server never approves, so the browser blocks the real request; a "simple"
//     form/text POST that skips preflight is rejected here for the wrong type.
//   - If an Origin header is present, it must be same-origin with the target.
//
// /dlna/ and the frontend/static routes are intentionally exempt: DLNA senders
// are not browsers and speak SOAP, and GET navigation must stay open.
func withGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutatingMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			if !hasJSONContentType(r) {
				rejectCrossSite(w, r)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				rejectCrossSite(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func hasJSONContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "application/json")
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func rejectCrossSite(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"detail":"` + i18n.Request(r, "cross_site_blocked") + `"}`))
}

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
// English action label, skipping the props heartbeat and image fetches. Logs
// are shared diagnostic artifacts, so they must not follow request/UI language.
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
		log.Printf("  %s [%-6s] %-14s  %5dms  HTTP %d", mark, r.Method, actionLabel(r.Method, path), ms, rec.status)
	})
}

func actionLabel(method, path string) string {
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
			"state": "Get player state", "play": "Play", "command": "Player command",
			"stop": "Stop", "props": "Get player props",
		}
		if v, ok := m[sub]; ok {
			return v
		}
	case "emby":
		m := map[string]string{
			"libraries": "Get libraries", "resume": "Get recent items",
			"items": "Get media items", "episodes": "Get episodes",
		}
		if v, ok := m[sub]; ok {
			return v
		}
		return "Emby " + sub
	case "servers":
		if sub == "" {
			if method == "GET" {
				return "List Media Sources"
			}
			return "Add Media Source"
		}
		switch tail {
		case "activate":
			return "Switch Media Source"
		case "host":
			return "Switch IP"
		case "login":
			return "Sign in server"
		case "":
			if method == "PUT" || method == "PATCH" {
				return "Edit Media Source"
			}
			if method == "DELETE" {
				return "Delete Media Source"
			}
			return "Get Media Source"
		}
	case "settings":
		return "Settings"
	}
	return path
}
