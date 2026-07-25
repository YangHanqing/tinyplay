package server

import (
	"net/http"
	"strings"

	"tvremote/internal/auth"
	"tvremote/internal/i18n"
)

// tokenCookieName carries the device token on requests a phone cannot attach a
// header to. Poster images are plain <img src>, so the fetch-level header alone
// would leave every artwork request unauthenticated. The cookie is set by the
// frontend from the same token it keeps in localStorage, and cross-site abuse of
// it is already blocked by withGuard plus SameSite=Strict.
const tokenCookieName = "tp_token"

// withAuth requires a paired device for the JSON API.
//
// Exempt on purpose:
//
//   - Everything outside /api/ — the frontend shell has to load before a phone
//     can pair at all, /dlna/ senders are not browsers and cannot hold a token,
//     and /desktop/ is guarded by its own loopback check.
//   - /api/pair/ — the pairing handshake is what produces a token.
//   - Loopback callers — the native shell and mpv run on the same machine and
//     already have full access to it; a token would protect nothing.
func withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			strings.HasPrefix(r.URL.Path, "/api/pair/") ||
			isLoopbackRequest(r) ||
			auth.Default.Verify(requestToken(r)) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// The frontend switches to its pairing screen on this header, without
		// having to guess why a request was refused.
		w.Header().Set("X-TinyPlay-Pairing", "required")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"` + i18n.Request(r, "pair_required") + `","pairing_required":true}`))
	})
}

func requestToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); header != "" {
		if token, ok := cutBearer(header); ok {
			return token
		}
	}
	if header := r.Header.Get("X-TinyPlay-Token"); header != "" {
		return header
	}
	if cookie, err := r.Cookie(tokenCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

func cutBearer(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):]), true
	}
	return "", false
}
