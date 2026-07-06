package iptv

import (
	"os"
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
		t.Fatalf("got %d channels, want 3 (two CCTV-1 lines dedupe into one channel with 2 variants)", len(channels))
	}

	cctv1 := channels[0]
	if cctv1.Name != "CCTV-1 综合 HD" || cctv1.TvgID != "cctv1.cn" || cctv1.GroupTitle != "央视" {
		t.Errorf("unexpected cctv1 channel: %+v", cctv1)
	}
	if cctv1.LogoURL != "https://example.com/cctv1.png" {
		t.Errorf("logo url not parsed: %+v", cctv1)
	}
	if len(cctv1.Variants) != 2 {
		t.Fatalf("cctv1 variants = %d, want 2 (HD + 4K dedupe by tvg-id)", len(cctv1.Variants))
	}
	if cctv1.Variants[0].Label != "HD" || cctv1.Variants[1].Label != "4K" {
		t.Errorf("unexpected variant labels: %+v", cctv1.Variants)
	}

	cctv5 := channels[1]
	if cctv5.TvgID != "cctv5.cn" || cctv5.Quality != "" {
		t.Errorf("unexpected cctv5 channel: %+v", cctv5)
	}

	unknown := channels[2]
	if unknown.TvgID != "" {
		t.Errorf("channel with no tvg-id should have an empty TvgID, got %q", unknown.TvgID)
	}
	if unknown.ID == "" {
		t.Errorf("channel with no tvg-id should still get a derived hash id")
	}
}

func TestParseQuality(t *testing.T) {
	cases := map[string]string{
		"CCTV-1 综合 HD":   "HD",
		"CCTV-1 综合 4K":   "4K",
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
