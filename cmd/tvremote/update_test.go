package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseUpdateVersion(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"v0.9.8", true}, {"0.9.8", true}, {"1.10.0", true},
		{"dev", false}, {"0.9", false}, {"0.09.8", false},
		{"v0.9.8-rc.1", false}, {"0.9.8+build", false},
	} {
		_, ok := parseUpdateVersion(tc.raw)
		if ok != tc.want {
			t.Errorf("parseUpdateVersion(%q) ok=%v, want %v", tc.raw, ok, tc.want)
		}
	}
}

func TestFindUpdateUsesAPIAndComparesSemver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.9.10","html_url":"https://github.com/YangHanqing/tinyplay/releases/tag/v0.9.10"}`))
	}))
	defer server.Close()

	release, err := findUpdate(context.Background(), "0.9.9", server.Client(), updateEndpoints{API: server.URL + "/latest", Page: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if release == nil || release.Version != "v0.9.10" {
		t.Fatalf("release = %#v", release)
	}

	release, err = findUpdate(context.Background(), "0.9.10", server.Client(), updateEndpoints{API: server.URL + "/latest", Page: server.URL})
	if err != nil || release != nil {
		t.Fatalf("same version: release=%#v err=%v", release, err)
	}
}

func TestReleaseTagFromURLAcceptsOnlyTinyPlayGitHubRelease(t *testing.T) {
	valid, _ := url.Parse("https://github.com/YangHanqing/tinyplay/releases/tag/v1.0.0")
	if tag, ok := releaseTagFromURL(valid); !ok || tag != "v1.0.0" {
		t.Fatalf("valid tag = %q, %v", tag, ok)
	}
	for _, raw := range []string{
		"https://example.com/YangHanqing/tinyplay/releases/tag/v1.0.0",
		"https://github.com/YangHanqing/tinyplay/releases/latest",
	} {
		u, _ := url.Parse(raw)
		if tag, ok := releaseTagFromURL(u); ok || tag != "" {
			t.Errorf("releaseTagFromURL(%q) = %q, %v", raw, tag, ok)
		}
	}
}
