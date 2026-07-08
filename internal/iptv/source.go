package iptv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tvremote/internal/config"
)

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string   { return e.Msg }
func (e *APIError) StatusCode() int { return e.Status }
func errf(status int, format string, args ...any) *APIError {
	return &APIError{status, fmt.Sprintf(format, args...)}
}

// Summary is the source-card status shown in settings (channel/EPG counts,
// last refresh, ok/error).
type Summary struct {
	ChannelCount    int    `json:"channel_count"`
	EPGMatchedCount int    `json:"epg_matched_count"`
	LastRefreshed   string `json:"last_refreshed,omitempty"`
	LastAttempt     string `json:"last_attempt,omitempty"`
	RefreshStatus   string `json:"refresh_status"` // "pending" | "ok" | "error"
	RefreshError    string `json:"refresh_error,omitempty"`
}

type cacheSnapshot struct {
	Channels   []Channel   `json:"channels"`
	Programmes []Programme `json:"programmes"`
	Summary    Summary     `json:"summary"`
}

// Client is the per-server IPTV accessor, modeled on filesource.Client.
type Client struct{ server *config.Server }

func New(server *config.Server) *Client { return &Client{server: server} }

// FromActive returns a Client for the active server, erroring if it is not
// an IPTV source (mirrors filesource.FromActive).
func FromActive() (*Client, error) {
	s := config.ActiveServer()
	if s == nil || config.NormalizeServerType(s.Type) != "iptv" {
		return nil, errf(400, "No IPTV source is available")
	}
	return New(s), nil
}

// FromServer returns a Client for a specific server id, for endpoints that
// take an explicit ?server_id= rather than assuming the active source.
func FromServer(id string) (*Client, error) {
	s := config.GetServer(id)
	if s == nil || config.NormalizeServerType(s.Type) != "iptv" {
		return nil, errf(400, "No such IPTV source")
	}
	return New(s), nil
}

// staleAfter controls the refresh-on-access policy: reads never block on a
// fetch, but Channels()/Summary() etc. trigger a background Refresh once the
// cache is older than this, so the source stays roughly current without a
// dedicated background ticker wired into the app's startup path.
const staleAfter = 6 * time.Hour
const automaticRetryAfter = 5 * time.Minute

var (
	cacheMu   sync.RWMutex
	caches    = map[string]*cacheSnapshot{}
	refreshMu sync.Mutex
	refreshes = map[string]*refreshCall{}

	httpClient = &http.Client{Timeout: 90 * time.Second}
)

type refreshCall struct {
	done chan struct{}
	err  error
}

func cachePath(serverID string) string {
	return filepath.Join(config.DataDir(), "iptv-cache", serverID+".json")
}

func (c *Client) load() *cacheSnapshot {
	cacheMu.RLock()
	snap, ok := caches[c.server.ID]
	cacheMu.RUnlock()
	if ok {
		return snap
	}
	if raw, err := os.ReadFile(cachePath(c.server.ID)); err == nil {
		var snap cacheSnapshot
		if json.Unmarshal(raw, &snap) == nil {
			cacheMu.Lock()
			caches[c.server.ID] = &snap
			cacheMu.Unlock()
			return &snap
		}
	}
	return &cacheSnapshot{Summary: Summary{RefreshStatus: "pending"}}
}

func (c *Client) store(snap *cacheSnapshot) {
	cacheMu.Lock()
	caches[c.server.ID] = snap
	cacheMu.Unlock()
	_ = os.MkdirAll(filepath.Dir(cachePath(c.server.ID)), 0o755)
	if buf, err := json.Marshal(snap); err == nil {
		_ = os.WriteFile(cachePath(c.server.ID), buf, 0o644)
	}
}

// Refresh fetches and reparses the playlist and (if configured, or hinted by
// the playlist's own x-tvg-url) the EPG, replacing the cache atomically. EPG
// fetch/parse failure degrades gracefully: the channel list still ships with
// has_epg effectively false for every channel, it does not fail the refresh.
func (c *Client) Refresh(ctx context.Context) error {
	// Coalesce concurrent summary/channel reads and manual refreshes for the
	// same source. A slow provider must never be fetched several times merely
	// because the phone loads summary, categories, and channels in parallel.
	refreshMu.Lock()
	if active := refreshes[c.server.ID]; active != nil {
		refreshMu.Unlock()
		select {
		case <-active.done:
			return active.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	refreshes[c.server.ID] = call
	refreshMu.Unlock()

	call.err = c.refreshOnce(ctx)
	refreshMu.Lock()
	delete(refreshes, c.server.ID)
	close(call.done)
	refreshMu.Unlock()
	return call.err
}

func (c *Client) refreshOnce(ctx context.Context) error {
	attemptedAt := time.Now().UTC().Format(time.RFC3339)
	previous := c.load()
	storeFailure := func(err error) error {
		// Keep the last known-good channel/EPG payload. A transient network
		// failure should be visible in status, but must not turn a working IPTV
		// source into an empty, permanently non-refreshing cache.
		failed := *previous
		failed.Summary.LastAttempt = attemptedAt
		failed.Summary.RefreshStatus = "error"
		failed.Summary.RefreshError = err.Error()
		c.store(&failed)
		return err
	}

	if strings.TrimSpace(c.server.PlaylistURL) == "" {
		err := errf(400, "A playlist URL is required")
		return storeFailure(err)
	}
	channels, epgHint, err := c.fetchPlaylist(ctx)
	if err != nil {
		return storeFailure(err)
	}
	epgURL := firstNonEmpty(strings.TrimSpace(c.server.EPGURL), epgHint)
	var programmes []Programme
	if epgURL != "" {
		if p, err := c.fetchEPG(ctx, epgURL); err == nil {
			programmes = p
		}
	}
	c.store(&cacheSnapshot{
		Channels:   channels,
		Programmes: programmes,
		Summary: Summary{
			ChannelCount:    len(channels),
			EPGMatchedCount: matchedChannelCount(channels, programmes),
			LastRefreshed:   time.Now().UTC().Format(time.RFC3339),
			LastAttempt:     attemptedAt,
			RefreshStatus:   "ok",
		},
	})
	return nil
}

func matchedChannelCount(channels []Channel, programmes []Programme) int {
	ids := make(map[string]bool, len(programmes))
	for _, p := range programmes {
		ids[p.ChannelID] = true
	}
	count := 0
	for _, ch := range channels {
		if ch.TvgID != "" && ids[strings.ToLower(ch.TvgID)] {
			count++
		}
	}
	return count
}

func fetchURL(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errf(400, "Invalid URL: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errf(502, "Could not fetch %s: %v", rawURL, err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, errf(502, "%s returned HTTP %d", rawURL, resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *Client) fetchPlaylist(ctx context.Context) ([]Channel, string, error) {
	body, err := fetchURL(ctx, c.server.PlaylistURL)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	r, err := maybeGunzip(body)
	if err != nil {
		return nil, "", errf(502, "Could not decompress playlist: %v", err)
	}
	return ParseM3U(r)
}

func (c *Client) fetchEPG(ctx context.Context, epgURL string) ([]Programme, error) {
	body, err := fetchURL(ctx, epgURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	r, err := maybeGunzip(body)
	if err != nil {
		return nil, errf(502, "Could not decompress EPG: %v", err)
	}
	return ParseXMLTV(r)
}

// ensureFresh kicks off a non-blocking Refresh when the cache has never been
// populated is left alone here (the add-source flow does a synchronous
// Refresh instead) — this only handles the "gone stale" case.
func (c *Client) ensureFresh() {
	snap := c.load()
	if !automaticRefreshDue(snap.Summary, time.Now()) {
		return
	}
	go func() { _ = c.Refresh(context.Background()) }()
}

func automaticRefreshDue(summary Summary, now time.Time) bool {
	if summary.LastRefreshed == "" {
		return false
	}
	lastSuccess, err := time.Parse(time.RFC3339, summary.LastRefreshed)
	if err != nil || now.Sub(lastSuccess) < staleAfter {
		return false
	}
	if summary.LastAttempt == "" {
		return true
	}
	lastAttempt, err := time.Parse(time.RFC3339, summary.LastAttempt)
	return err != nil || now.Sub(lastAttempt) >= automaticRetryAfter
}

func (c *Client) Channels() []Channel {
	c.ensureFresh()
	return c.load().Channels
}

// Categories returns the distinct raw group-title values found in the
// playlist, in first-seen order. Built-in pseudo-categories (all/favorites/
// recent) are the caller's responsibility to prepend, since only the caller
// (the HTTP handler) knows the current server's favorites/recents.
func (c *Client) Categories() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ch := range c.Channels() {
		if ch.GroupTitle == "" || seen[ch.GroupTitle] {
			continue
		}
		seen[ch.GroupTitle] = true
		out = append(out, ch.GroupTitle)
	}
	return out
}

func (c *Client) Summary() Summary {
	c.ensureFresh()
	return c.load().Summary
}

func (c *Client) ChannelByID(id string) *Channel {
	for _, ch := range c.Channels() {
		if ch.ID == id {
			ch := ch
			return &ch
		}
	}
	return nil
}

// CurrentProgramme returns the programme airing right now for a channel, or
// nil when the channel has no tvg-id or no matching EPG entry — this nil is
// the graceful no-EPG fallback the frontend renders as a blank "now playing".
func (c *Client) CurrentProgramme(channelID string) *Programme {
	ch := c.ChannelByID(channelID)
	if ch == nil || ch.TvgID == "" {
		return nil
	}
	now := time.Now()
	for _, p := range c.load().Programmes {
		if p.ChannelID == strings.ToLower(ch.TvgID) && !now.Before(p.Start) && now.Before(p.Stop) {
			p := p
			return &p
		}
	}
	return nil
}

func (c *Client) UpcomingProgrammes(channelID string, n int) []Programme {
	ch := c.ChannelByID(channelID)
	if ch == nil || ch.TvgID == "" {
		return nil
	}
	now := time.Now()
	var out []Programme
	for _, p := range c.load().Programmes {
		if p.ChannelID == strings.ToLower(ch.TvgID) && p.Start.After(now) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
