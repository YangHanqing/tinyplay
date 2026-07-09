package emby

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"tvremote/internal/config"
)

func TestJellyfinUsesCanonicalAuthorizationHeader(t *testing.T) {
	c := New(&config.Server{Type: "jellyfin", DeviceID: "device"})
	h := c.headers("token")
	if h.Get("Authorization") == "" || h.Get("X-Emby-Authorization") != "" {
		t.Fatalf("headers=%v", h)
	}
}

func TestClientRemainsBoundWhenActiveServerChanges(t *testing.T) {
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[]}`))
	}))
	defer upstream.Close()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())

	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	a := config.AddServer(config.Server{Name: "A", Type: "emby", Protocol: u.Scheme,
		Hosts: []string{u.Hostname()}, Port: port, UserID: "user-a"})
	b := config.AddServer(config.Server{Name: "B", Type: "emby", Protocol: "http",
		Hosts: []string{"127.0.0.1"}, Port: 1, UserID: "user-b"})
	client := New(a)
	if !config.SetActiveServer(b.ID) {
		t.Fatal("could not activate B")
	}
	if _, err := client.Libraries(); err != nil {
		t.Fatalf("client followed active server instead of remaining on A: %v", err)
	}
	if requests != 1 {
		t.Fatalf("A requests = %d, want 1", requests)
	}
}
