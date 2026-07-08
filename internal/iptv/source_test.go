package iptv

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestRefreshFailurePreservesLastKnownGoodSnapshot(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	lastSuccess := time.Now().Add(-7 * time.Hour).UTC().Format(time.RFC3339)
	c := &Client{server: &config.Server{ID: "refresh-preserve", PlaylistURL: server.URL}}
	store(t, c, &cacheSnapshot{
		Channels:   []Channel{{ID: "old", Name: "Old but playable"}},
		Programmes: []Programme{{ChannelID: "old", Title: "Cached programme"}},
		Summary: Summary{
			ChannelCount:  1,
			LastRefreshed: lastSuccess,
			RefreshStatus: "ok",
		},
	})

	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want upstream failure")
	}
	got := c.load()
	if len(got.Channels) != 1 || got.Channels[0].ID != "old" {
		t.Fatalf("channels after failed refresh = %+v, want cached channel", got.Channels)
	}
	if len(got.Programmes) != 1 || got.Programmes[0].Title != "Cached programme" {
		t.Fatalf("programmes after failed refresh = %+v, want cached programme", got.Programmes)
	}
	if got.Summary.LastRefreshed != lastSuccess {
		t.Fatalf("last_refreshed = %q, want %q", got.Summary.LastRefreshed, lastSuccess)
	}
	if got.Summary.LastAttempt == "" || got.Summary.RefreshStatus != "error" || got.Summary.RefreshError == "" {
		t.Fatalf("failure summary = %+v, want attempted/error state", got.Summary)
	}
}

func TestAutomaticRefreshDueThrottlesFailures(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-staleAfter - time.Minute).Format(time.RFC3339)

	if automaticRefreshDue(Summary{LastRefreshed: stale, LastAttempt: now.Add(-time.Minute).Format(time.RFC3339)}, now) {
		t.Fatal("automaticRefreshDue = true during failure retry throttle")
	}
	if !automaticRefreshDue(Summary{LastRefreshed: stale, LastAttempt: now.Add(-automaticRetryAfter - time.Second).Format(time.RFC3339)}, now) {
		t.Fatal("automaticRefreshDue = false after failure retry throttle")
	}
	if automaticRefreshDue(Summary{}, now) {
		t.Fatal("automaticRefreshDue = true for a never-populated source")
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
