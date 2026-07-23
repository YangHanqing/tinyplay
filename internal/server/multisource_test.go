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
	req := jsonReq(http.MethodPost, "/api/servers", body)
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

func TestJellyfinEmptyPasswordAuthenticatesOnCreate(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)

	var authenticated bool
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/Users/AuthenticateByName" {
			http.NotFound(w, r)
			return
		}
		var credentials struct {
			Username string `json:"Username"`
			Password string `json:"Pw"`
		}
		if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
			t.Fatalf("decode credentials: %v", err)
		}
		if credentials.Username != "demo" || credentials.Password != "" {
			t.Fatalf("got credentials %#v, want demo with an empty password", credentials)
		}
		authenticated = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"AccessToken":"demo-token","User":{"Id":"demo-user"}}`))
	}))
	defer demo.Close()

	demoURL, err := url.Parse(demo.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(demoURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	h := New(player.New()).Handler()
	body, err := json.Marshal(map[string]any{
		"name": "Jellyfin demo", "type": "jellyfin", "protocol": demoURL.Scheme,
		"hosts": []string{demoURL.Hostname()}, "port": port,
		"username": "demo", "password": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := jsonReq(http.MethodPost, "/api/servers", string(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	if !authenticated {
		t.Fatal("Jellyfin sign-in was not attempted")
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if userID, _ := created["user_id"].(string); userID != "demo-user" {
		t.Fatalf("user_id = %q, want demo-user", userID)
	}
}

func TestLocalFolderPickerCanBrowseBeforeSourceExists(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	h := New(player.New()).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?source_type=local", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview browse %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"breadcrumb"`) || !strings.Contains(rec.Body.String(), `"entries"`) {
		t.Fatalf("unexpected preview listing: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("preview browsing must not create a source: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIPTVChannelAPIDoesNotExposeStreamCredentials(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	playlist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegurl")
		fmt.Fprint(w, `#EXTM3U
#EXTINF:-1 tvg-id="news" tvg-name="News",News
https://stream.example/live.m3u8?token=stream-secret|User-Agent=PrivateAgent&Cookie=session%3Dprivate-cookie
`)
	}))
	defer playlist.Close()

	h := New(player.New()).Handler()
	body, err := json.Marshal(map[string]any{
		"name": "Private playlist", "type": "iptv", "playlist_url": playlist.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := jsonReq(http.MethodPost, "/api/servers", string(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	for _, path := range []string{
		"/api/iptv/channels?server_id=" + url.QueryEscape(id),
		"/api/iptv/channel/tvg:news?server_id=" + url.QueryEscape(id),
	} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		response := rec.Body.String()
		for _, secret := range []string{"stream-secret", "PrivateAgent", "private-cookie", "stream.example"} {
			if strings.Contains(response, secret) {
				t.Fatalf("GET %s leaked %q: %s", path, secret, response)
			}
		}
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
	req := jsonReq(http.MethodPost, "/api/servers", string(createBody))
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
	req = jsonReq(http.MethodPost, "/api/servers/"+id+"/connect", `{}`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body.String())
	}

	// Finalize: PUT the chosen sub-path.
	req = jsonReq(http.MethodPut, "/api/servers/"+id, `{"root_path":"Movies"}`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"root_path":"Movies"`) {
		t.Fatalf("finalize PUT: %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidatedEditKeepsWorkingFileSourceOnFailure(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	media := filepath.Join(data, "media")
	if err := os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	h := New(player.New()).Handler()

	req := jsonReq(http.MethodPost, "/api/servers", `{"name":"Local","type":"local","root_path":`+jsonString(media)+`}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)

	missing := filepath.Join(data, "does-not-exist")
	req = jsonReq(http.MethodPut, "/api/servers/"+id, `{"root_path":`+jsonString(missing)+`,"validate":true}`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("invalid validated edit unexpectedly succeeded: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), jsonString(media)) || strings.Contains(rec.Body.String(), jsonString(missing)) {
		t.Fatalf("failed edit should preserve original source: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFileListHonorsExplicitServerID(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	first := filepath.Join(data, "first")
	second := filepath.Join(data, "second")
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "first.mkv"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.mkv"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(player.New()).Handler()

	create := func(name, root string) string {
		req := jsonReq(http.MethodPost, "/api/servers", `{"name":`+jsonString(name)+`,"type":"local","root_path":`+jsonString(root)+`}`)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
		var server map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &server); err != nil {
			t.Fatal(err)
		}
		return server["id"].(string)
	}
	_ = create("first", first) // stays process-active
	secondID := create("second", second)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?server_id="+url.QueryEscape(secondID), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"second.mkv"`) || strings.Contains(rec.Body.String(), `"name":"first.mkv"`) {
		t.Fatalf("explicit source list: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsReset(t *testing.T) {
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	h := New(player.New()).Handler()

	body := `{"name":"Home","type":"emby","hosts":["nas.local"]}`
	req := jsonReq(http.MethodPost, "/api/servers", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}

	req = jsonReq(http.MethodPost, "/api/settings/reset", `{}`)
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
