package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	body := `{"name":"Local","type":"local","root_path":` + jsonString(media) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"local"`) || !strings.Contains(rec.Body.String(), `"logged_in":true`) {
		t.Fatalf("servers: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/files/list?path=", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"demo.mkv"`) || !strings.Contains(rec.Body.String(), `"is_video":true`) {
		t.Fatalf("list: %s", rec.Body.String())
	}
}

// TestFileSourceCreateThenBrowseByServerIDThenFinalize exercises the actual
// add-source flow the folder picker relies on: create a source with no
// root_path (just enough to connect), browse it by server_id before it's
// active, then PUT the chosen path once — matching create → connect →
// browse → pick → finalize.
func TestFileSourceCreateThenBrowseByServerIDThenFinalize(t *testing.T) {
	var authOK bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		authOK = ok && u == "alice" && p == "secret"
		if r.Method != "PROPFIND" {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		self := r.URL.Path
		fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response><d:response><d:href>%sMovies/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`, self, self)
	}))
	defer ts.Close()
	tsURL, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(tsURL.Port())

	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	h := New(player.New()).Handler()

	createBody, _ := json.Marshal(map[string]any{
		"name": "DAV", "type": "webdav", "protocol": tsURL.Scheme,
		"hosts": []string{tsURL.Hostname()}, "port": port,
		"username": "alice", "password": "secret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(string(createBody)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create (no root_path) %d: %s", rec.Code, rec.Body.String())
	}
	if !authOK {
		t.Fatal("create should have verified credentials against the WebDAV server")
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)
	if created["root_path"] != "" {
		t.Fatalf("expected empty root_path right after creation, got %#v", created["root_path"])
	}

	// Browse it by server_id — it isn't the active server yet.
	req = httptest.NewRequest(http.MethodGet, "/api/files/list?server_id="+id, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"Movies"`) {
		t.Fatalf("browse by server_id: %d %s", rec.Code, rec.Body.String())
	}

	// Reconnect (e.g. after editing credentials in the UI) without persisting.
	req = httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/connect", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body.String())
	}

	// Finalize: PUT the chosen sub-path.
	req = httptest.NewRequest(http.MethodPut, "/api/servers/"+id, strings.NewReader(`{"root_path":"Movies"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"root_path":"Movies"`) {
		t.Fatalf("finalize PUT: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsReset(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	h := New(player.New()).Handler()

	body := `{"name":"Home","type":"emby","hosts":["nas.local"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/settings/reset", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("reset %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected no servers after reset, got: %s", rec.Body.String())
	}
}

func jsonString(value string) string { b, _ := json.Marshal(value); return string(b) }
