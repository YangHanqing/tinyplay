package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

func feedbackReportBody(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics/report", nil)
	rec := httptest.NewRecorder()
	testHandler(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	return out
}

// The reason this endpoint exists: /api/player/debug-report answers 404 when
// nothing has ever played, which is exactly the state a user is in when they
// report that a source will not connect. Feedback must still produce a report.
func TestFeedbackReportAnswersWithoutAnyPlayback(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := New(player.New())

	req := httptest.NewRequest(http.MethodGet, "/api/player/debug-report", nil)
	rec := httptest.NewRecorder()
	testHandler(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("playback debug-report should still 404 without a session, got %d", rec.Code)
	}

	report := feedbackReportBody(t, s)
	playback, ok := report["playback"].(map[string]any)
	if !ok {
		t.Fatalf("playback section missing: %v", report["playback"])
	}
	if playback["available"] != false {
		t.Fatalf("playback.available = %v, want false", playback["available"])
	}
	app, ok := report["app"].(map[string]any)
	if !ok {
		t.Fatalf("app section missing: %v", report["app"])
	}
	for _, key := range []string{"version", "platform", "arch", "uptime_seconds", "data_dir_kind"} {
		if _, present := app[key]; !present {
			t.Fatalf("app.%s missing from report: %v", key, app)
		}
	}
}

func TestFeedbackReportCarriesBuildVersion(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	s := New(player.New())
	s.SetVersion("1.2.3")
	app := feedbackReportBody(t, s)["app"].(map[string]any)
	if app["version"] != "1.2.3" {
		t.Fatalf("app.version = %v, want 1.2.3", app["version"])
	}
}

// The report travels to a stranger's inbox by way of the user's clipboard, so
// the shape of a source may go but its identity may not.
func TestFeedbackReportDescribesSourcesWithoutIdentifyingThem(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	added := config.AddServer(config.Server{
		Name:        "Living room NAS",
		Type:        "emby",
		Protocol:    "http",
		Hosts:       []string{"192.168.31.9"},
		Port:        8096,
		Username:    "hanqing",
		Password:    "hunter2",
		AccessToken: "tok-secret",
		UserID:      "user-1",
	})
	config.SetActiveServer(added.ID)
	config.AddServer(config.Server{
		Name:     "Movies",
		Type:     "smb",
		Hosts:    []string{"nas.local"},
		Port:     445,
		Share:    "Media",
		RootPath: "Movies",
		Username: "guest",
		Password: "pw",
	})

	s := New(player.New())
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics/report", nil)
	rec := httptest.NewRecorder()
	testHandler(s).ServeHTTP(rec, req)
	raw := rec.Body.String()

	for _, secret := range []string{
		"Living room NAS", "Movies", "192.168.31.9", "nas.local",
		"hanqing", "hunter2", "tok-secret", "user-1", "Media",
	} {
		if strings.Contains(raw, secret) {
			t.Fatalf("report leaked %q: %s", secret, raw)
		}
	}

	sources := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}
	section := sources["sources"].(map[string]any)
	if section["count"].(float64) != 2 {
		t.Fatalf("sources.count = %v, want 2", section["count"])
	}
	list := section["list"].([]any)
	first := list[0].(map[string]any)
	if first["type"] != "emby" || first["active"] != true || first["signed_in"] != true {
		t.Fatalf("emby source shape wrong: %v", first)
	}
	if first["host_kind"] != "ip" {
		t.Fatalf("host_kind = %v, want ip", first["host_kind"])
	}
	second := list[1].(map[string]any)
	if second["type"] != "smb" || second["has_share"] != true || second["has_root_path"] != true {
		t.Fatalf("smb source shape wrong: %v", second)
	}
	if second["host_kind"] != "hostname" {
		t.Fatalf("host_kind = %v, want hostname", second["host_kind"])
	}
}
