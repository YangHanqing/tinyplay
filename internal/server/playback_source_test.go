package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

func TestActivatingAnotherLibraryDoesNotStopPlayback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test player uses a POSIX shell")
	}
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)
	script := filepath.Join(data, "fake-player.sh")
	if err := os.WriteFile(script, []byte("sleep 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TVREMOTE_MPV_EXE", "/bin/sh")

	a := config.AddServer(config.Server{Name: "A", Type: "emby", Hosts: []string{"a-primary", "a-backup"}})
	b := config.AddServer(config.Server{Name: "B", Type: "emby", Hosts: []string{"b-primary", "b-backup"}})
	if !config.SetActiveServer(a.ID) {
		t.Fatal("could not activate A")
	}
	p := player.New()
	if result := p.Play(script, player.PlayOptions{ServerID: a.ID, ItemID: "movie-a"}); result["ok"] != true {
		t.Fatalf("play: %#v", result)
	}

	h := New(p).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/servers/"+b.ID+"/activate", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	state := p.State()
	if state["server_id"] != a.ID || state["item_id"] != "movie-a" {
		t.Fatalf("playback changed after browsing switch: %#v", state)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/servers/"+b.ID+"/host", strings.NewReader(`{"host_index":1}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch browsing host: %d %s", rec.Code, rec.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	state = p.State()
	if state["server_id"] != a.ID || state["item_id"] != "movie-a" {
		t.Fatalf("playback changed after unrelated host switch: %#v", state)
	}
	p.Stop()
	time.Sleep(time.Second)
}
