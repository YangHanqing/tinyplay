package iptv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
// last refresh, ok/error). EPG has its own status: a provider's guide can be
// temporarily unavailable while the live channel list remains fresh.
type Summary struct {
	ChannelCount    int    `json:"channel_count"`
	EPGMatchedCount int    `json:"epg_matched_count"`
	LastRefreshed   string `json:"last_refreshed,omitempty"`
	LastAttempt     string `json:"last_attempt,omitempty"`
	RefreshStatus   string `json:"refresh_status"` // "pending" | "ok" | "error"
	RefreshError    string `json:"refresh_error,omitempty"`
	EPGStatus       string `json:"epg_status,omitempty"` // "none" | "ok" | "stale" | "error"
	EPGError        string `json:"epg_error,omitempty"`
}

type cacheSnapshot struct {
	Channels   []Channel   `json:"channels"`
	Programmes []Programme `json:"programmes"`
	Summary    Summary     `json:"summary"`
	// EPGSource distinguishes a failed refresh of the same guide (where stale
	// data is useful) from a user changing/removing the guide URL (where keeping
	// the old schedule would be misleading).
	EPGSource string `json:"epg_source,omitempty"`

	channelIndex      map[string]int
	programmesByTvgID map[string][]Programme
}

// Client is the per-server IPTV accessor, modeled on filesource.Client.
type Client struct{ server *config.Server }

func New(server *config.Server) *Client { return &Client{server: server} }

// ServerID lets HTTP handlers keep favorites/recents scoped to the same source
// that resolved a channel, including an explicit ?server_id browse request.
func (c *Client) ServerID() string { return c.server.ID }

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

const (
	maximumPlaylistBytes         = 24 * 1024 * 1024
	maximumExpandedPlaylistBytes = 64 * 1024 * 1024
	maximumEPGBytes              = 48 * 1024 * 1024
	maximumExpandedEPGBytes      = 128 * 1024 * 1024
	maximumRedirects             = 5
)

var (
	cacheMu   sync.RWMutex
	caches    = map[string]*cacheSnapshot{}
	refreshMu sync.Mutex
	refreshes = map[string]*refreshCall{}

	httpClient = &http.Client{Timeout: 60 * time.Second}
)

type refreshCall struct {
	done chan struct{}
	err  error
}

func cachePath(serverID string) string {
	// Server IDs are generated UUIDs, but use Base defensively so a damaged
	// config cannot turn cache deletion into an arbitrary filesystem operation.
	return filepath.Join(config.DataDir(), "iptv-cache", filepath.Base(serverID)+".json")
}

func indexedSnapshot(snap *cacheSnapshot) *cacheSnapshot {
	if snap == nil {
		snap = &cacheSnapshot{}
	}
	snap.channelIndex = make(map[string]int, len(snap.Channels))
	for i := range snap.Channels {
		snap.channelIndex[snap.Channels[i].ID] = i
	}
	snap.programmesByTvgID = make(map[string][]Programme)
	for _, programme := range snap.Programmes {
		programme.ChannelID = strings.ToLower(strings.TrimSpace(programme.ChannelID))
		if programme.ChannelID == "" || !programme.Stop.After(programme.Start) {
			continue
		}
		snap.programmesByTvgID[programme.ChannelID] = append(snap.programmesByTvgID[programme.ChannelID], programme)
	}
	for key := range snap.programmesByTvgID {
		sort.SliceStable(snap.programmesByTvgID[key], func(i, j int) bool {
			return snap.programmesByTvgID[key][i].Start.Before(snap.programmesByTvgID[key][j].Start)
		})
	}
	if snap.Summary.RefreshStatus == "" {
		snap.Summary.RefreshStatus = "pending"
	}
	return snap
}

func (c *Client) load() *cacheSnapshot {
	cacheMu.RLock()
	snap, ok := caches[c.server.ID]
	cacheMu.RUnlock()
	if ok {
		// Tests and older in-memory callers may have populated a pre-index
		// snapshot directly. Normalize it once before exposing it to reads.
		if snap.channelIndex == nil || snap.programmesByTvgID == nil {
			cacheMu.Lock()
			snap = indexedSnapshot(snap)
			caches[c.server.ID] = snap
			cacheMu.Unlock()
		}
		return snap
	}
	if raw, err := os.ReadFile(cachePath(c.server.ID)); err == nil {
		var decoded cacheSnapshot
		if json.Unmarshal(raw, &decoded) == nil {
			snap = indexedSnapshot(&decoded)
			cacheMu.Lock()
			caches[c.server.ID] = snap
			cacheMu.Unlock()
			return snap
		}
	}
	return indexedSnapshot(&cacheSnapshot{Summary: Summary{RefreshStatus: "pending", EPGStatus: "none"}})
}

func (c *Client) store(snap *cacheSnapshot) {
	snap = indexedSnapshot(snap)
	cacheMu.Lock()
	caches[c.server.ID] = snap
	cacheMu.Unlock()

	dir := filepath.Dir(cachePath(c.server.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	buf, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".iptv-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, cachePath(c.server.ID))
}

// RemoveCache removes a deleted source's persisted playlist, guide and any
// provider headers. It is intentionally best-effort: config deletion should
// not fail merely because a stale cache file is already gone.
func RemoveCache(serverID string) {
	cacheMu.Lock()
	delete(caches, serverID)
	cacheMu.Unlock()
	_ = os.Remove(cachePath(serverID))
}

// ClearCaches is the settings-reset companion to RemoveCache.
func ClearCaches() {
	cacheMu.Lock()
	caches = map[string]*cacheSnapshot{}
	cacheMu.Unlock()
	_ = os.RemoveAll(filepath.Join(config.DataDir(), "iptv-cache"))
}

// Refresh fetches and reparses the playlist and (if configured, or hinted by
// the playlist's own x-tvg-url) the EPG, replacing the cache atomically.
func (c *Client) Refresh(ctx context.Context) error {
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
		failed := *previous
		failed.Summary.LastAttempt = attemptedAt
		failed.Summary.RefreshStatus = "error"
		failed.Summary.RefreshError = publicError(err)
		c.store(&failed)
		return err
	}

	if strings.TrimSpace(c.server.PlaylistURL) == "" {
		return storeFailure(errf(400, "A playlist URL is required"))
	}
	channels, epgHint, err := c.fetchPlaylist(ctx)
	if err != nil {
		return storeFailure(err)
	}
	if len(channels) == 0 {
		return storeFailure(errf(502, "The playlist contains no playable channels"))
	}

	configuredEPG := strings.TrimSpace(c.server.EPGURL)
	var epgResource *ResourceRequest
	if configuredEPG != "" {
		resource, parseErr := parseResourceRequest(configuredEPG, nil)
		if parseErr != nil {
			return storeFailure(parseErr)
		}
		epgResource = &resource
	} else {
		epgResource = epgHint
	}

	programmes := []Programme{}
	epgSource := ""
	epgStatus := "none"
	epgError := ""
	if epgResource != nil {
		epgSource = resourceFingerprint(*epgResource)
		if programmes, err = c.fetchEPG(ctx, *epgResource); err != nil {
			// Reuse only a guide from the same configured/hinted feed. Retaining
			// an old provider's schedule after the user changes URLs is worse than
			// showing no guide at all.
			if previous.EPGSource == epgSource && len(previous.Programmes) > 0 {
				programmes = append([]Programme(nil), previous.Programmes...)
				epgStatus = "stale"
			} else {
				epgStatus = "error"
			}
			epgError = publicError(err)
		} else {
			epgStatus = "ok"
		}
	}

	c.store(&cacheSnapshot{
		Channels:   channels,
		Programmes: programmes,
		EPGSource:  epgSource,
		Summary: Summary{
			ChannelCount:    len(channels),
			EPGMatchedCount: matchedChannelCount(channels, programmes),
			LastRefreshed:   time.Now().UTC().Format(time.RFC3339),
			LastAttempt:     attemptedAt,
			RefreshStatus:   "ok",
			EPGStatus:       epgStatus,
			EPGError:        epgError,
		},
	})
	return nil
}

func publicError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errInputTooLarge) {
		return "The response is too large to load safely"
	}
	if e, ok := err.(*APIError); ok {
		return e.Msg
	}
	return "The source could not be refreshed"
}

func resourceFingerprint(resource ResourceRequest) string {
	if resource.URL == nil {
		return ""
	}
	keys := make([]string, 0, len(resource.HTTPHeaders))
	for key := range resource.HTTPHeaders {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{resource.URL.String()}
	for _, key := range keys {
		parts = append(parts, key+"="+resource.HTTPHeaders[key])
	}
	return strings.Join(parts, "\x00")
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

type fetchedResource struct {
	Body      io.ReadCloser
	FinalURL  *url.URL
	SetCookie string
}

type readerCloser struct {
	io.Reader
	io.Closer
}

func fetchURL(ctx context.Context, resource ResourceRequest, maximumBytes int64) (fetchedResource, error) {
	if !httpURL(resource) {
		return fetchedResource{}, errf(400, "Only HTTP and HTTPS source URLs are supported")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL.String(), nil)
	if err != nil {
		return fetchedResource{}, errf(400, "Invalid source URL")
	}
	req.Header.Set("User-Agent", "TinyPlay/1.0")
	req.Header.Set("Accept", "application/x-mpegURL, application/vnd.apple.mpegurl, text/plain, application/xml, text/xml, */*")
	for name, value := range resource.HTTPHeaders {
		req.Header.Set(name, value)
	}

	client := *httpClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= maximumRedirects {
			return http.ErrUseLastResponse
		}
		if !sameHost(resource.URL, next.URL) {
			next.Header.Del("Cookie")
			next.Header.Del("Authorization")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fetchedResource{}, err
		}
		return fetchedResource{}, errf(502, "Could not fetch source")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		host := resource.URL.Hostname()
		if host == "" {
			host = "source"
		}
		return fetchedResource{}, errf(502, "%s returned HTTP %d", host, resp.StatusCode)
	}
	if resp.ContentLength > maximumBytes {
		resp.Body.Close()
		return fetchedResource{}, errInputTooLarge
	}
	return fetchedResource{
		Body:      readerCloser{Reader: newBoundedReader(resp.Body, maximumBytes), Closer: resp.Body},
		FinalURL:  resp.Request.URL,
		SetCookie: cookieHeader(resp.Cookies()),
	}, nil
}

// cookieHeader translates the standard Set-Cookie response form into the
// one request header mpv understands. It is only merged through
// inheritedHeadersForStream below, which keeps it on the playlist's final
// host rather than leaking a provider session to a CDN entry.
func cookieHeader(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" || strings.ContainsAny(cookie.Name+cookie.Value, "\r\n") {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func (c *Client) fetchPlaylist(ctx context.Context) ([]Channel, *ResourceRequest, error) {
	resource, err := parseResourceRequest(c.server.PlaylistURL, nil)
	if err != nil {
		return nil, nil, err
	}
	fetched, err := fetchURL(ctx, resource, maximumPlaylistBytes)
	if err != nil {
		return nil, nil, err
	}
	defer fetched.Body.Close()
	r, err := maybeGunzip(fetched.Body)
	if err != nil {
		return nil, nil, errf(502, "Could not decompress playlist")
	}
	inheritedHeaders := map[string]string{}
	mergeHeaders(inheritedHeaders, resource.HTTPHeaders)
	if fetched.SetCookie != "" {
		if existing := inheritedHeaders["Cookie"]; existing != "" {
			fetched.SetCookie = existing + "; " + fetched.SetCookie
		}
		addAllowedHeader(inheritedHeaders, "Cookie", fetched.SetCookie)
	}
	channels, hint, err := ParseM3UWithResources(newBoundedReader(r, maximumExpandedPlaylistBytes), fetched.FinalURL, inheritedHeaders)
	if err != nil {
		if errors.Is(err, errInputTooLarge) {
			return nil, nil, errInputTooLarge
		}
		return nil, nil, errf(502, "Could not parse playlist")
	}
	return channels, hint, nil
}

func (c *Client) fetchEPG(ctx context.Context, resource ResourceRequest) ([]Programme, error) {
	fetched, err := fetchURL(ctx, resource, maximumEPGBytes)
	if err != nil {
		return nil, err
	}
	defer fetched.Body.Close()
	r, err := maybeGunzip(fetched.Body)
	if err != nil {
		return nil, errf(502, "Could not decompress EPG")
	}
	programmes, err := ParseXMLTV(newBoundedReader(r, maximumExpandedEPGBytes))
	if err != nil {
		if errors.Is(err, errInputTooLarge) {
			return nil, errInputTooLarge
		}
		return nil, errf(502, "Could not parse EPG")
	}
	return programmes, nil
}

// ensureFresh kicks off a non-blocking Refresh. The first read after a missing
// or damaged cache also schedules a fetch; a failed source is then throttled
// rather than being left permanently pending until the user finds Refresh.
func (c *Client) ensureFresh() {
	snap := c.load()
	if !automaticRefreshDue(snap.Summary, time.Now()) {
		return
	}
	go func() { _ = c.Refresh(context.Background()) }()
}

func automaticRefreshDue(summary Summary, now time.Time) bool {
	if summary.LastRefreshed == "" {
		if summary.LastAttempt == "" {
			return true
		}
		lastAttempt, err := time.Parse(time.RFC3339, summary.LastAttempt)
		return err != nil || now.Sub(lastAttempt) >= automaticRetryAfter
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
	channels := c.load().Channels
	return append([]Channel(nil), channels...)
}

// Categories returns the distinct raw group-title values found in the
// playlist, in first-seen order. Built-in pseudo-categories (all/favorites/
// recent) are the caller's responsibility to prepend.
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
	c.ensureFresh()
	snap := c.load()
	index, ok := snap.channelIndex[id]
	if !ok || index < 0 || index >= len(snap.Channels) {
		return nil
	}
	channel := snap.Channels[index]
	return &channel
}

func shiftedProgramme(programme Programme, shiftHours float64) Programme {
	if shiftHours == 0 {
		return programme
	}
	shift := time.Duration(shiftHours * float64(time.Hour))
	programme.Start = programme.Start.Add(shift)
	programme.Stop = programme.Stop.Add(shift)
	return programme
}

func currentProgrammeForChannel(snap *cacheSnapshot, channel Channel, now time.Time) *Programme {
	if channel.TvgID == "" {
		return nil
	}
	for _, raw := range snap.programmesByTvgID[strings.ToLower(channel.TvgID)] {
		programme := shiftedProgramme(raw, channel.EPGShiftHours)
		if !now.Before(programme.Start) && now.Before(programme.Stop) {
			return &programme
		}
	}
	return nil
}

// CurrentProgramme returns the programme airing right now for a channel, or
// nil when the channel has no tvg-id or no matching EPG entry.
func (c *Client) CurrentProgramme(channelID string) *Programme {
	c.ensureFresh()
	snap := c.load()
	index, ok := snap.channelIndex[channelID]
	if !ok {
		return nil
	}
	return currentProgrammeForChannel(snap, snap.Channels[index], time.Now())
}

// CurrentProgrammes evaluates a whole filtered channel list against the
// snapshot's O(1) channel and per-tvg guide indexes. HTTP handlers use this
// once per response, eliminating the old O(channels² + channels×EPG) path.
func (c *Client) CurrentProgrammes(channels []Channel) map[string]Programme {
	c.ensureFresh()
	snap := c.load()
	now := time.Now()
	out := make(map[string]Programme)
	for _, channel := range channels {
		if programme := currentProgrammeForChannel(snap, channel, now); programme != nil {
			out[channel.ID] = *programme
		}
	}
	return out
}

func (c *Client) UpcomingProgrammes(channelID string, n int) []Programme {
	c.ensureFresh()
	snap := c.load()
	index, ok := snap.channelIndex[channelID]
	if !ok || snap.Channels[index].TvgID == "" {
		return nil
	}
	channel := snap.Channels[index]
	now := time.Now()
	out := []Programme{}
	for _, raw := range snap.programmesByTvgID[strings.ToLower(channel.TvgID)] {
		programme := shiftedProgramme(raw, channel.EPGShiftHours)
		if programme.Start.After(now) {
			out = append(out, programme)
			if n > 0 && len(out) == n {
				break
			}
		}
	}
	return out
}

// PastProgrammes returns the most recently finished guide entries first. It is
// separate from UpcomingProgrammes so a phone/native guide can offer archive
// playback without fetching or retaining an unbounded schedule in its view.
func (c *Client) PastProgrammes(channelID string, n int) []Programme {
	c.ensureFresh()
	snap := c.load()
	index, ok := snap.channelIndex[channelID]
	if !ok || snap.Channels[index].TvgID == "" {
		return nil
	}
	channel := snap.Channels[index]
	now := time.Now()
	out := []Programme{}
	entries := snap.programmesByTvgID[strings.ToLower(channel.TvgID)]
	for i := len(entries) - 1; i >= 0; i-- {
		programme := shiftedProgramme(entries[i], channel.EPGShiftHours)
		if !programme.Stop.After(now) {
			out = append(out, programme)
			if n > 0 && len(out) == n {
				break
			}
		}
	}
	return out
}
