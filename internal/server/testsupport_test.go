package server

import "net/http"

// testHandler builds the full router for tests that exercise handler behaviour
// rather than the pairing boundary.
//
// httptest.NewRequest stamps a public sample address (192.0.2.1) onto every
// request, which withAuth correctly treats as an unpaired device on the network.
// These tests are about what the handlers do once a caller is allowed in, so the
// request is presented as coming from this machine — the same exemption the
// native shell and mpv rely on. The boundary itself is covered by
// TestPairingBoundary* in auth_middleware_test.go.
func testHandler(s *Server) http.Handler {
	handler := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "127.0.0.1:54321"
		handler.ServeHTTP(w, r)
	})
}
