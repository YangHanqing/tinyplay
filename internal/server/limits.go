package server

import "net/http"

// The phone remote only sends small JSON control payloads. Keep limits at the
// HTTP boundary so malformed or slow LAN peers cannot consume unbounded memory
// before a handler has a chance to decode their request.
const (
	maximumRequestBodyBytes = 1 << 20
	maximumConcurrentHTTP   = 32
)

func withRequestLimits(next http.Handler) http.Handler {
	semaphore := make(chan struct{}, maximumConcurrentHTTP)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		default:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"detail": "The remote is busy; please try again.",
			})
			return
		}

		if r.ContentLength > maximumRequestBodyBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"detail": "Request body is too large.",
			})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maximumRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}
