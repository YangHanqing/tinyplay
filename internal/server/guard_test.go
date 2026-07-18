package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonReq builds a request that passes withGuard, matching what the phone
// frontend's api() helper sends (Content-Type: application/json).
func jsonReq(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func guardHarness() http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return withGuard(ok)
}

func TestWithGuard(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		origin      string
		want        int
	}{
		{"get is always open", http.MethodGet, "/api/player/state", "", "", http.StatusOK},
		{"json post allowed", http.MethodPost, "/api/player/command", "application/json", "", http.StatusOK},
		{"json post with charset allowed", http.MethodPost, "/api/player/command", "application/json; charset=utf-8", "", http.StatusOK},
		{"same-origin post allowed", http.MethodPost, "/api/player/command", "application/json", "http://192.168.1.5:1980", http.StatusOK},
		{"text post blocked", http.MethodPost, "/api/player/command", "text/plain", "", http.StatusForbidden},
		{"missing content-type blocked", http.MethodPost, "/api/player/command", "", "", http.StatusForbidden},
		{"cross-origin post blocked", http.MethodPost, "/api/player/command", "application/json", "http://evil.example", http.StatusForbidden},
		{"dlna soap exempt", http.MethodPost, "/dlna/AVTransport/control", "text/xml", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Host = "192.168.1.5:1980"
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			guardHarness().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("got %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
