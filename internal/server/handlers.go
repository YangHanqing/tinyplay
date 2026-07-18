package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"tvremote/internal/i18n"
)

// This file holds the request/response plumbing shared by every handler. The
// handlers themselves are split by domain into handlers_*.go (servers, player,
// library, files, iptv, system, frontend).

// ── response helpers ─────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRaw(w http.ResponseWriter, r *http.Request, body []byte, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	lang := i18n.RequestLang(r)
	var apiErr interface{ StatusCode() int }
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.StatusCode(), map[string]string{"detail": i18n.LocalizeError(lang, err.Error())})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": i18n.LocalizeError(lang, err.Error())})
}

func decode(r *http.Request, v any) bool {
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func qInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// invalidBody is the shared 400 for an unparseable JSON request body.
func invalidBody(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
}

// clientForRequest resolves the ?server_id= query param to a source client,
// falling back to the active source when it's absent. The add-source folder
// picker and the IPTV/library browsers pass server_id explicitly to reach a
// source that isn't active yet; the common case omits it. Shared by
// mediaClient / filesClient / iptvClient.
func clientForRequest[T any](r *http.Request, fromServer func(string) (T, error), active func() (T, error)) (T, error) {
	if id := r.URL.Query().Get("server_id"); id != "" {
		return fromServer(id)
	}
	return active()
}
