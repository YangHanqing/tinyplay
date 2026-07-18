package iptv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !automaticRefreshDue(Summary{}, now) {
		t.Fatal("automaticRefreshDue = false for a never-populated source")
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

func TestEPGFailureOnlyRetainsTheSameGuide(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	var guideOK = true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=one,One\nhttps://stream.example/one.m3u8\n"))
		case "/guide-a":
			if !guideOK {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`<tv><programme channel="one" start="20260706080000 +0000" stop="20260706090000 +0000"><title>A</title></programme></tv>`))
		case "/guide-b":
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	c := New(&config.Server{ID: "epg-source", PlaylistURL: server.URL + "/list", EPGURL: server.URL + "/guide-a"})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	guideOK = false
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("same-guide refresh should keep live channels: %v", err)
	}
	if got := c.load(); got.Summary.EPGStatus != "stale" || len(got.Programmes) != 1 {
		t.Fatalf("same-guide failure = %#v programmes=%d", got.Summary, len(got.Programmes))
	}

	c.server.EPGURL = server.URL + "/guide-b"
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("new-guide refresh should keep live channels: %v", err)
	}
	if got := c.load(); got.Summary.EPGStatus != "error" || len(got.Programmes) != 0 {
		t.Fatalf("new-guide failure should clear stale guide: %#v programmes=%d", got.Summary, len(got.Programmes))
	}
}

func TestCatchupStreamExpandsAndValidatesReplayWindow(t *testing.T) {
	now := time.Now().UTC()
	c := &Client{server: &config.Server{ID: "catchup"}}
	store(t, c, &cacheSnapshot{Channels: []Channel{{
		ID: "one", Name: "One", CatchupSource: "https://archive.example/replay?start=${start}&end=$end&duration=$duration",
		CatchupDays: 2,
		Variants:    []StreamVariant{{URL: "https://stream.example/live", Label: "HD", HTTPHeaders: map[string]string{"Referer": "https://app.example/"}}},
	}}})
	start := now.Add(-90 * time.Minute)
	stop := start.Add(30 * time.Minute)
	stream, err := c.CatchupStream("one", 0, start, stop)
	if err != nil {
		t.Fatal(err)
	}
	if want := "start=" + fmt.Sprint(start.Unix()); !strings.Contains(stream.URL, want) || !strings.Contains(stream.URL, "duration=1800") {
		t.Fatalf("expanded URL = %q", stream.URL)
	}
	if stream.HTTPHeaders["Referer"] != "https://app.example/" {
		t.Fatalf("headers = %#v", stream.HTTPHeaders)
	}
	if _, err := c.CatchupStream("one", 0, now.Add(-3*24*time.Hour), now.Add(-3*24*time.Hour+time.Hour)); err == nil {
		t.Fatal("old programme unexpectedly accepted")
	}
}

func TestPlaylistSessionCookieStaysOnPlaylistHost(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "playlist-cookie", Path: "/"})
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,News\n/live/news.m3u8\n"))
	}))
	defer server.Close()
	c := &Client{server: &config.Server{ID: "playlist-cookie", PlaylistURL: server.URL + "/list.m3u"}}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	channels := c.Channels()
	if len(channels) != 1 {
		t.Fatalf("channels = %#v", channels)
	}
	if got := channels[0].Variants[0].HTTPHeaders["Cookie"]; got != "session=playlist-cookie" {
		t.Fatalf("stream cookie = %q", got)
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
