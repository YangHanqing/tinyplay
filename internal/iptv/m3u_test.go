package iptv

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestParseM3U(t *testing.T) {
	f, err := os.Open("testdata/sample.m3u")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	channels, epgHint, err := ParseM3U(f)
	if err != nil {
		t.Fatalf("ParseM3U: %v", err)
	}
	if epgHint != "https://example.com/guide.xml.gz" {
		t.Errorf("epgHint = %q, want the #EXTM3U x-tvg-url attribute", epgHint)
	}
	if len(channels) != 3 {
		t.Fatalf("got %d channels, want 3 (two News-1 lines dedupe into one channel with 2 variants)", len(channels))
	}

	news1 := channels[0]
	if news1.Name != "News-1 HD" || news1.TvgID != "news1.example" || news1.GroupTitle != "News" {
		t.Errorf("unexpected news1 channel: %+v", news1)
	}
	if news1.LogoURL != "https://example.com/news1.png" {
		t.Errorf("logo url not parsed: %+v", news1)
	}
	if len(news1.Variants) != 2 {
		t.Fatalf("news1 variants = %d, want 2 (HD + 4K dedupe by tvg-id)", len(news1.Variants))
	}
	if news1.Variants[0].Label != "HD" || news1.Variants[1].Label != "4K" {
		t.Errorf("unexpected variant labels: %+v", news1.Variants)
	}

	sports1 := channels[1]
	if sports1.TvgID != "sports1.example" || sports1.Quality != "" {
		t.Errorf("unexpected sports1 channel: %+v", sports1)
	}

	unknown := channels[2]
	if unknown.TvgID != "" {
		t.Errorf("channel with no tvg-id should have an empty TvgID, got %q", unknown.TvgID)
	}
	if unknown.ID == "" {
		t.Errorf("channel with no tvg-id should still get a derived hash id")
	}
}

func TestParseM3UWithRealWorldExtensions(t *testing.T) {
	base, err := url.Parse("https://provider.example/list/index.m3u")
	if err != nil {
		t.Fatal(err)
	}
	input := `#EXTM3U url-tvg="../guide.xml|User-Agent=GuideAgent"
#EXTINF:-1 tvg-id='news.example' tvg-name=News tvg-shift=8 catchup="append" catchup-days=7 catchup-source="../archive/news?start=${start}&duration=$duration|Referer=https%3A%2F%2Fapp.example%2F",News HD
#EXTGRP:Live
#EXTVLCOPT:http-referrer=https://app.example/
#EXTVLCOPT:http-user-agent=StreamAgent
streams/news.m3u8|Cookie=stream%3Dtoken
`
	channels, hint, err := ParseM3UWithResources(strings.NewReader(input), base, map[string]string{
		"Cookie": "playlist=session", "Authorization": "Bearer playlist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if hint == nil || hint.URL.String() != "https://provider.example/guide.xml" || hint.HTTPHeaders["User-Agent"] != "GuideAgent" {
		t.Fatalf("EPG hint = %#v, want resolved URL and header", hint)
	}
	if len(channels) != 1 {
		t.Fatalf("channels = %#v", channels)
	}
	ch := channels[0]
	if ch.GroupTitle != "Live" || ch.EPGShiftHours != 8 || ch.Catchup != "append" || ch.CatchupDays != 7 {
		t.Fatalf("extension metadata = %#v", ch)
	}
	if got := ch.Variants[0].URL; got != "https://provider.example/list/streams/news.m3u8" {
		t.Fatalf("relative URL = %q", got)
	}
	if ch.CatchupSource != "https://provider.example/archive/news?start=${start}&duration=$duration" ||
		ch.CatchupHeaders["Referer"] != "https://app.example/" {
		t.Fatalf("catch-up source = %q headers=%#v", ch.CatchupSource, ch.CatchupHeaders)
	}
	headers := ch.Variants[0].HTTPHeaders
	if headers["Referer"] != "https://app.example/" || headers["User-Agent"] != "StreamAgent" ||
		headers["Cookie"] != "stream=token" || headers["Authorization"] != "Bearer playlist" {
		t.Fatalf("stream headers = %#v", headers)
	}
}

func TestParseQuality(t *testing.T) {
	cases := map[string]string{
		"News-1 HD":      "HD",
		"News-1 4K":      "4K",
		"BBC News FHD":   "FHD",
		"BBC News 1080p": "FHD",
		"Plain Channel":  "",
	}
	for name, want := range cases {
		if got := parseQuality(name); got != want {
			t.Errorf("parseQuality(%q) = %q, want %q", name, got, want)
		}
	}
}
