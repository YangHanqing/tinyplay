package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/config"
)

func TestWithRequestLimitsRejectsDeclaredOversizeBody(t *testing.T) {
	h := withRequestLimits(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not receive an oversized request")
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/player/command", strings.NewReader("{}"))
	req.ContentLength = maximumRequestBodyBytes + 1
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSafeServerReportsCredentialStateWithoutPassword(t *testing.T) {
	safe := safeServer(&config.Server{ID: "source-1", Password: "source-secret"})
	if _, found := safe["password"]; found {
		t.Fatal("server response must not include the stored password")
	}
	if saved, _ := safe["password_saved"].(bool); !saved {
		t.Fatal("server response should report that a password is saved")
	}
}
