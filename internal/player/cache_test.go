package player

import "testing"

func TestPlaybackCacheModeFor(t *testing.T) {
	cases := []struct {
		name, sourceType string
		want             playbackCacheMode
	}{
		{"LAN Emby", "emby", cacheOnDemand},
		{"Plex", "plex", cacheOnDemand},
		{"SMB", "smb", cacheOnDemand},
		{"WebDAV", "webdav", cacheOnDemand},
		{"NFS", "nfs", cacheOnDemand},
		{"DLNA URL", "dlna", cacheOnDemand},
		{"local folder", "local", cacheDisabled},
		{"IPTV", "iptv", cacheLive},
		{"IPTV catch-up", "iptv-catchup", cacheOnDemand},
	}
	for _, tc := range cases {
		if got := playbackCacheModeFor(tc.sourceType); got != tc.want {
			t.Errorf("%s: playbackCacheModeFor() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCacheConfigIsMemoryBounded(t *testing.T) {
	secs, forward, back := cacheConfig()
	if secs == 0 || forward > 512*1024*1024 || back > 64*1024*1024 {
		t.Fatalf("cacheConfig() = (%d, %d, %d), want bounded values", secs, forward, back)
	}
}
