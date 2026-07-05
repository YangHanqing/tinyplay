package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tvremote/internal/player"
)

func TestFileSourceCRUDAndBrowseContract(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	media := filepath.Join(data, "media")
	if err := os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "demo.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(player.New()).Handler()
	body := `{"name":"Local","type":"file","file_protocol":"local","root":` + jsonString(media) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"file"`) || !strings.Contains(rec.Body.String(), `"logged_in":true`) {
		t.Fatalf("servers: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/files/list?path=", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"demo.mkv"`) || !strings.Contains(rec.Body.String(), `"is_video":true`) {
		t.Fatalf("list: %s", rec.Body.String())
	}
}

func jsonString(value string) string { b, _ := json.Marshal(value); return string(b) }
