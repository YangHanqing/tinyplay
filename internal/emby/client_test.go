package emby

import (
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
