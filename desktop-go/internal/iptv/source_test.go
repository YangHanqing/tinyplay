package iptv

import (
	"testing"
	"time"

	"tvremote/internal/config"
)

func TestMatchedChannelCount(t *testing.T) {
	channels := []Channel{
		{ID: "a", TvgID: "cctv1.cn"},
		{ID: "b", TvgID: "cctv5.cn"},
		{ID: "c", TvgID: ""}, // no tvg-id: can never match, must not count
	}
	programmes := []Programme{
		{ChannelID: "cctv1.cn"},
		{ChannelID: "cctv1.cn"},
		{ChannelID: "unrelated.channel"},
	}
	if got := matchedChannelCount(channels, programmes); got != 1 {
		t.Errorf("matchedChannelCount = %d, want 1", got)
	}
}

func TestCurrentAndUpcomingProgrammes(t *testing.T) {
	now := time.Now()
	snap := &cacheSnapshot{
		Channels: []Channel{
			{ID: "chan-with-epg", TvgID: "cctv1.cn"},
			{ID: "chan-without-epg", TvgID: ""},
		},
		Programmes: []Programme{
			{ChannelID: "cctv1.cn", Start: now.Add(-30 * time.Minute), Stop: now.Add(30 * time.Minute), Title: "Now Airing"},
			{ChannelID: "cctv1.cn", Start: now.Add(30 * time.Minute), Stop: now.Add(90 * time.Minute), Title: "Next Up"},
			{ChannelID: "cctv1.cn", Start: now.Add(-2 * time.Hour), Stop: now.Add(-1 * time.Hour), Title: "Already Over"},
		},
	}
	c := &Client{server: &config.Server{ID: "test-server"}}
	store(t, c, snap)

	cur := c.CurrentProgramme("chan-with-epg")
	if cur == nil || cur.Title != "Now Airing" {
		t.Fatalf("CurrentProgramme = %+v, want Now Airing", cur)
	}

	upcoming := c.UpcomingProgrammes("chan-with-epg", 5)
	if len(upcoming) != 1 || upcoming[0].Title != "Next Up" {
		t.Fatalf("UpcomingProgrammes = %+v, want [Next Up]", upcoming)
	}

	// A channel with no tvg-id must degrade to nil/empty, never an error.
	if got := c.CurrentProgramme("chan-without-epg"); got != nil {
		t.Errorf("CurrentProgramme for a channel with no tvg-id = %+v, want nil", got)
	}
	if got := c.UpcomingProgrammes("chan-without-epg", 5); got != nil {
		t.Errorf("UpcomingProgrammes for a channel with no tvg-id = %+v, want nil", got)
	}
}

// store writes directly into the package-level cache map, bypassing disk I/O,
// so these tests don't depend on config.DataDir()/filesystem state.
func store(t *testing.T, c *Client, snap *cacheSnapshot) {
	t.Helper()
	cacheMu.Lock()
	caches[c.server.ID] = snap
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		delete(caches, c.server.ID)
		cacheMu.Unlock()
	})
}
