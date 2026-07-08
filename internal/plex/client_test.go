package plex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tvremote/internal/config"
)

func plexTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	u := strings.TrimPrefix(ts.URL, "http://")
	return New(&config.Server{Type: "plex", Protocol: "http", Hosts: []string{u[:strings.LastIndex(u, ":")]}, Port: mustPort(u), AccessToken: "token"}), ts
}
func mustPort(hostport string) int {
	var p int
	fmtSscanf(hostport[strings.LastIndex(hostport, ":")+1:], &p)
	return p
}
func fmtSscanf(s string, p *int) { _, _ = fmt.Sscanf(s, "%d", p) }

func TestPlexNormalizesLibrariesAndPlayback(t *testing.T) {
	var timeline bool
	c, ts := plexTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "token" {
			t.Errorf("missing token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Directory": []any{map[string]any{"key": "1", "title": "Movies", "type": "movie"}}}})
		case "/library/metadata/42":
			json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Metadata": []any{map[string]any{"ratingKey": "42", "title": "Demo", "type": "movie", "duration": 120000, "viewOffset": 30000, "Media": []any{map[string]any{"Part": []any{map[string]any{"id": "part", "key": "/library/parts/1/file.mkv"}}}}}}}})
		case "/:/timeline":
			timeline = true
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	})
	defer ts.Close()
	body, err := c.Libraries()
	if err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	if err = json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list["TotalRecordCount"].(float64) != 1 {
		t.Fatalf("%s", body)
	}
	choice, err := c.ChoosePlayURL("42", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(choice.URL, "X-Plex-Token=token") || choice.MediaSourceID != "part" {
		t.Fatalf("%#v", choice)
	}
	if got := c.ResumePositionSeconds("42"); got != 30 {
		t.Fatalf("resume=%v", got)
	}
	c.ReportProgress("42", "", 5_000_000, true, "")
	if !timeline {
		t.Fatal("timeline not reported")
	}
}
