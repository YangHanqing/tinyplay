package main

// The desktop shells use this small release checker instead of scraping the
// GitHub releases HTML. It deliberately only discovers a newer version: the
// actual installation remains in the user's browser, where macOS DMGs and the
// portable Windows ZIP have their normal, explicit install flow.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tinyPlayReleaseAPI  = "https://api.github.com/repos/YangHanqing/tinyplay/releases/latest"
	tinyPlayReleasePage = "https://github.com/YangHanqing/tinyplay/releases/latest"
	updateUserAgent     = "TinyPlay update checker"
)

type updateRelease struct {
	Version string
	PageURL string
}

type updateEndpoints struct {
	API  string
	Page string
}

// findTinyPlayUpdate returns the latest stable release only when it is newer
// than the running build. A GitHub API failure falls back to the documented
// /releases/latest redirect, which is useful on networks where api.github.com
// and github.com do not fail in the same way.
func findTinyPlayUpdate(ctx context.Context, currentVersion string) (*updateRelease, error) {
	return findUpdate(ctx, currentVersion, newUpdateHTTPClient(), updateEndpoints{
		API:  tinyPlayReleaseAPI,
		Page: tinyPlayReleasePage,
	})
}

func findUpdate(ctx context.Context, currentVersion string, client *http.Client, endpoints updateEndpoints) (*updateRelease, error) {
	current, ok := parseUpdateVersion(currentVersion)
	if !ok {
		// Local and development builds must never offer a public release as an
		// "update". It would be surprising while running `go run`.
		return nil, nil
	}

	release, err := fetchLatestRelease(ctx, client, endpoints.API)
	if err != nil {
		release, err = fetchLatestReleaseRedirect(ctx, client, endpoints.Page)
	}
	if err != nil {
		return nil, err
	}
	latest, ok := parseUpdateVersion(release.Version)
	if !ok {
		return nil, fmt.Errorf("latest release has an invalid version: %q", release.Version)
	}
	if compareUpdateVersions(latest, current) <= 0 {
		return nil, nil
	}
	if release.PageURL == "" {
		release.PageURL = endpoints.Page
	}
	return release, nil
}

func newUpdateHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: 4 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (*updateRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("latest release returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Draft || payload.Prerelease || payload.TagName == "" {
		return nil, errors.New("latest release is not a published stable release")
	}
	pageURL, err := url.Parse(payload.HTMLURL)
	if err != nil {
		return nil, err
	}
	if tag, ok := releaseTagFromURL(pageURL); !ok || tag != payload.TagName {
		return nil, errors.New("latest release returned an unexpected release URL")
	}
	return &updateRelease{Version: payload.TagName, PageURL: payload.HTMLURL}, nil
}

func fetchLatestReleaseRedirect(ctx context.Context, client *http.Client, endpoint string) (*updateRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("latest release page returned HTTP %d", resp.StatusCode)
	}
	finalURL := resp.Request.URL
	version, ok := releaseTagFromURL(finalURL)
	if !ok {
		return nil, errors.New("latest release page did not redirect to a release tag")
	}
	return &updateRelease{Version: version, PageURL: finalURL.String()}, nil
}

func releaseTagFromURL(u *url.URL) (string, bool) {
	if u == nil || !strings.EqualFold(u.Host, "github.com") {
		return "", false
	}
	const prefix = "/YangHanqing/tinyplay/releases/tag/"
	if !strings.HasPrefix(u.EscapedPath(), prefix) {
		return "", false
	}
	version, err := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), prefix))
	return version, err == nil && version != ""
}

type updateVersion struct {
	major int
	minor int
	patch int
}

func parseUpdateVersion(raw string) (updateVersion, bool) {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	// Public update checks intentionally ignore prerelease and build metadata.
	// Releases/latest never returns prereleases, but rejecting them here keeps a
	// malformed redirect or API response from changing that policy.
	if v == "" || strings.ContainsAny(v, "+-") {
		return updateVersion{}, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return updateVersion{}, false
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return updateVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return updateVersion{}, false
		}
		values[i] = n
	}
	return updateVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

func compareUpdateVersions(a, b updateVersion) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}
