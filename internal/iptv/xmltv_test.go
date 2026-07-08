package iptv

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"
	"time"
)

func TestParseXMLTV(t *testing.T) {
	f, err := os.Open("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	programmes, err := ParseXMLTV(f)
	if err != nil {
		t.Fatalf("ParseXMLTV: %v", err)
	}
	// 4 <programme> elements in the fixture, one with an invalid start time
	// that must be skipped rather than failing the whole parse.
	if len(programmes) != 3 {
		t.Fatalf("got %d programmes, want 3 (one malformed entry skipped)", len(programmes))
	}
	if programmes[0].ChannelID != "cctv1.cn" || programmes[0].Title != "新闻联播" {
		t.Errorf("unexpected first programme: %+v", programmes[0])
	}
	if programmes[0].Desc != "今日要闻" {
		t.Errorf("desc not parsed: %+v", programmes[0])
	}
	wantStart := time.Date(2026, 7, 6, 19, 0, 0, 0, time.FixedZone("", 8*3600))
	if !programmes[0].Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", programmes[0].Start, wantStart)
	}
}

func TestMaybeGunzipRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := maybeGunzip(&buf)
	if err != nil {
		t.Fatalf("maybeGunzip: %v", err)
	}
	programmes, err := ParseXMLTV(r)
	if err != nil {
		t.Fatalf("ParseXMLTV on gunzipped input: %v", err)
	}
	if len(programmes) != 3 {
		t.Fatalf("got %d programmes from gzipped fixture, want 3", len(programmes))
	}
}

func TestParseXMLTVTime(t *testing.T) {
	tm, err := parseXMLTVTime("20260706083000 +0800")
	if err != nil {
		t.Fatal(err)
	}
	if tm.Hour() != 8 || tm.Minute() != 30 {
		t.Errorf("unexpected parsed time: %v", tm)
	}
	if _, err := parseXMLTVTime("garbage"); err == nil {
		t.Errorf("expected an error for an unparseable time")
	}
}
