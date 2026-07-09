// Package emby is the Go port of app/clients/emby.py: a standard Emby protocol
// client supporting multiple servers.
//
// IMPORTANT: this package must stay decoupled from the HTTP server and the mpv
// player. Its only internal dependency is config. That keeps it reusable for a
// future tvOS app (via gomobile, or mirrored into Swift) — see CLAUDE.md.
package emby

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tvremote/internal/config"
)

// APIError carries an HTTP status so handlers can map it to a response.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string   { return e.Msg }
func (e *APIError) StatusCode() int { return e.Status }

func errf(status int, format string, a ...any) *APIError {
	return &APIError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Client wraps a single Emby server. Build it with FromActive() in handlers.
type Client struct {
	server *config.Server
}

// PlayURLChoice is the selected Emby media source's direct-play URL. The URL
// itself may contain a token; don't log it.
type PlayURLChoice struct {
	URL           string
	MediaSourceID string
}

// FromActive returns a client bound to the active server, or an error if none.
func FromActive() (*Client, error) {
	srv := config.ActiveServer()
	if srv == nil {
		return nil, errf(400, "No Emby server is available. Add and sign in first.")
	}
	return &Client{server: srv}, nil
}

// New builds a client for an explicit server (used by Login).
func New(s *config.Server) *Client { return &Client{server: s} }

// reload re-reads the same server so token/host changes are picked up without
// allowing a library switch to redirect an existing playback session.
func (c *Client) reload() {
	if c.server == nil {
		return
	}
	if srv := config.GetServer(c.server.ID); srv != nil {
		c.server = srv
	}
}

func (c *Client) serverURL() string { return config.BuildServerURL(c.server) }
func (c *Client) userID() string    { return c.server.UserID }
func (c *Client) token() string     { return c.server.AccessToken }

func (c *Client) authHeaderValue(token string) string {
	deviceID := c.server.DeviceID
	if deviceID == "" {
		deviceID = "tv-remote-mpv-001"
	}
	version := c.server.ClientVersion
	if version == "" {
		version = "4.7.0.0"
	}
	parts := []string{
		`Client="TinyPlay"`,
		`Device="TinyPlay"`,
		`DeviceId="` + deviceID + `"`,
		`Version="` + version + `"`,
	}
	if token != "" {
		parts = append(parts, `Token="`+token+`"`)
	}
	return "MediaBrowser " + strings.Join(parts, ", ")
}

func (c *Client) headers(token string) http.Header {
	h := http.Header{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json; charset=utf-8"},
	}
	if c.server != nil && config.NormalizeServerType(c.server.Type) == "jellyfin" {
		h.Set("Authorization", c.authHeaderValue(token))
	} else {
		h.Set("X-Emby-Authorization", c.authHeaderValue(token))
	}
	return h
}

// makeURL builds serverURL + path with query params; optionally adds api_key.
func (c *Client) makeURL(path string, addToken bool, params url.Values) (string, error) {
	if c.serverURL() == "" {
		return "", errf(400, "No server address is configured")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.serverURL() + path
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			if v != "" {
				q.Add(k, v)
			}
		}
	}
	if addToken && c.token() != "" {
		q.Set("api_key", c.token())
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + q.Encode()
	}
	return u, nil
}

// pathCandidates tolerates both /emby-prefixed and bare endpoints.
func pathCandidates(path string, bareFirst bool) []string {
	var paths []string
	if strings.HasPrefix(path, "/emby/") {
		paths = []string{path, strings.Replace(path, "/emby", "", 1)}
	} else {
		p := path
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		paths = []string{"/emby" + p, p}
	}
	if bareFirst && len(paths) == 2 {
		paths[0], paths[1] = paths[1], paths[0]
	}
	return paths
}

// request performs the call, retrying across path candidates. Returns the raw
// response body so proxy handlers can pass it straight through.
func (c *Client) request(method, path string, params url.Values, body any) ([]byte, error) {
	c.reload()
	if c.serverURL() == "" {
		return nil, errf(400, "No Emby server is configured")
	}

	var lastErr error = errf(500, "Emby request failed")
	bareFirst := c.server != nil && config.NormalizeServerType(c.server.Type) == "jellyfin"
	for _, p := range pathCandidates(path, bareFirst) {
		u, err := c.makeURL(p, false, params)
		if err != nil {
			lastErr = err
			continue
		}
		var reader io.Reader
		if body != nil {
			buf, _ := json.Marshal(body)
			reader = bytes.NewReader(buf)
		}
		req, err := http.NewRequest(method, u, reader)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header = c.headers(c.token())
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == 401:
			lastErr = errf(401, "Authentication failed. Please sign in again.")
			continue
		case resp.StatusCode == 404:
			lastErr = errf(404, "Endpoint not found: %s", p)
			continue
		case resp.StatusCode >= 400:
			lastErr = errf(resp.StatusCode, "Emby returned HTTP %d", resp.StatusCode)
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

// ── Auth ─────────────────────────────────────────────────────────────────────

// Authenticate verifies credentials against srv and returns (accessToken,
// userID) WITHOUT persisting anything. It is used both by Login (which then
// persists) and by the "add server" flow (which only persists the server if
// this succeeds, so a wrong address/password never leaves a broken entry).
func Authenticate(srv *config.Server, username, password string) (string, string, error) {
	c := &Client{server: srv}
	if c.serverURL() == "" {
		return "", "", errf(400, "No server address is configured")
	}

	lastErr := "Login failed"
	paths := []string{"/emby/Users/AuthenticateByName", "/Users/AuthenticateByName"}
	if config.NormalizeServerType(srv.Type) == "jellyfin" {
		paths[0], paths[1] = paths[1], paths[0]
	}
	for _, path := range paths {
		buf, _ := json.Marshal(map[string]string{"Username": username, "Pw": password})
		req, err := http.NewRequest("POST", c.serverURL()+path, bytes.NewReader(buf))
		if err != nil {
			lastErr = err.Error()
			continue
		}
		req.Header = c.headers("")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			lastErr = "Incorrect username or password"
			continue
		}
		if resp.StatusCode == 404 {
			lastErr = "No Emby login endpoint found"
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
			continue
		}
		var out struct {
			AccessToken string `json:"AccessToken"`
			User        struct {
				ID string `json:"Id"`
			} `json:"User"`
		}
		_ = json.Unmarshal(data, &out)
		if out.AccessToken == "" || out.User.ID == "" {
			return "", "", errf(400, "Login succeeded but AccessToken/User.Id was missing")
		}
		return out.AccessToken, out.User.ID, nil
	}
	return "", "", errf(400, "Login failed: %s", lastErr)
}

// Login authenticates and persists token + user id onto an already-saved server.
func (c *Client) Login(serverID, username, password string) (map[string]any, error) {
	srv := config.GetServer(serverID)
	if srv == nil {
		return nil, errf(404, "Server not found")
	}
	token, userID, err := Authenticate(srv, username, password)
	if err != nil {
		return nil, err
	}
	config.SetAuth(serverID, username, token, userID)
	return map[string]any{"ok": true, "username": username}, nil
}

// ── Library (proxy: return raw JSON) ─────────────────────────────────────────

func (c *Client) Libraries() ([]byte, error) {
	return c.request("GET", "/Users/"+c.userID()+"/Views", nil, nil)
}

func (c *Client) Items(parentID, search string, start, limit int, includeEpisodes bool) ([]byte, error) {
	itemTypes := "Movie,Series,Video,BoxSet"
	if includeEpisodes {
		itemTypes = "Movie,Series,Episode,Video,BoxSet"
	}
	q := url.Values{
		"UserId":           {c.userID()},
		"Recursive":        {"true"},
		"IncludeItemTypes": {itemTypes},
		"Fields":           {"PrimaryImageAspectRatio,Overview,ProductionYear,RunTimeTicks,UserData,SeriesName,SeasonName,IndexNumber,ParentIndexNumber,CommunityRating,RecursiveItemCount,ChildCount"},
		"StartIndex":       {strconv.Itoa(start)},
		"Limit":            {strconv.Itoa(limit)},
	}
	if search != "" {
		q.Set("SearchTerm", search)
	} else {
		q.Set("SortBy", "ProductionYear")
		q.Set("SortOrder", "Descending")
	}
	if parentID != "" {
		q.Set("ParentId", parentID)
	}
	return c.request("GET", "/Users/"+c.userID()+"/Items", q, nil)
}

func (c *Client) Resume(start, limit int) ([]byte, error) {
	q := url.Values{
		"StartIndex":       {strconv.Itoa(start)},
		"Limit":            {strconv.Itoa(limit)},
		"Fields":           {"PrimaryImageAspectRatio,Overview,ProductionYear,RunTimeTicks,UserData,SeriesName,IndexNumber,ParentIndexNumber,SeasonId,SeriesId,CommunityRating"},
		"IncludeItemTypes": {"Episode,Movie,Video"},
		"MediaTypes":       {"Video"},
	}
	return c.request("GET", "/Users/"+c.userID()+"/Items/Resume", q, nil)
}

func (c *Client) ItemDetailRaw(itemID string) ([]byte, error) {
	q := url.Values{"Fields": {"PrimaryImageAspectRatio,Overview,ProductionYear,RunTimeTicks,Path,MediaSources,Genres,People,UserData,SeriesName,SeasonName,IndexNumber,ParentIndexNumber"}}
	return c.request("GET", "/Users/"+c.userID()+"/Items/"+itemID, q, nil)
}

func (c *Client) Episodes(seriesID, seasonID string, start, limit int, sort string) ([]byte, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	if start < 0 {
		start = 0
	}
	order := "Ascending"
	if sort == "desc" {
		order = "Descending"
	}
	q := url.Values{
		"UserId":     {c.userID()},
		"Fields":     {"Overview,RunTimeTicks,UserData,SeriesName,SeasonName,IndexNumber,ParentIndexNumber"},
		"StartIndex": {strconv.Itoa(start)},
		"Limit":      {strconv.Itoa(limit)},
		"SortBy":     {"IndexNumber"},
		"SortOrder":  {order},
	}
	if seasonID != "" {
		q.Set("SeasonId", seasonID)
	}
	return c.request("GET", "/Shows/"+seriesID+"/Episodes", q, nil)
}

func (c *Client) Seasons(seriesID string) ([]byte, error) {
	q := url.Values{"UserId": {c.userID()}}
	return c.request("GET", "/Shows/"+seriesID+"/Seasons", q, nil)
}

// ── Images ───────────────────────────────────────────────────────────────────

// ImageBytes returns the image content and its content-type, or nil if missing.
func (c *Client) ImageBytes(itemID string, maxHeight int, imageType string) ([]byte, string) {
	c.reload()
	if imageType == "" {
		imageType = "Primary"
	}
	data, ct := c.imageBytes(itemID, maxHeight, imageType, -1)
	if data != nil {
		return data, ct
	}
	if imageType != "Primary" {
		return c.ImageBytes(itemID, maxHeight, "Primary")
	}
	return nil, ""
}

// BackdropBytes returns one exact Emby backdrop index. Unlike ImageBytes, it
// does not fall back to Primary because the screensaver should use a backdrop or
// a black background.
func (c *Client) BackdropBytes(itemID string, maxHeight int, index int) ([]byte, string) {
	c.reload()
	return c.imageBytes(itemID, maxHeight, "Backdrop", index)
}

func (c *Client) imageBytes(itemID string, maxHeight int, imageType string, index int) ([]byte, string) {
	paths := []string{}
	if index >= 0 {
		idx := strconv.Itoa(index)
		paths = append(paths,
			"/emby/Items/"+itemID+"/Images/"+imageType+"/"+idx,
			"/Items/"+itemID+"/Images/"+imageType+"/"+idx,
		)
		if index == 0 {
			paths = append(paths,
				"/emby/Items/"+itemID+"/Images/"+imageType,
				"/Items/"+itemID+"/Images/"+imageType,
			)
		}
	} else {
		paths = append(paths,
			"/emby/Items/"+itemID+"/Images/"+imageType,
			"/Items/"+itemID+"/Images/"+imageType,
		)
	}
	for _, path := range paths {
		u, err := c.makeURL(path, false, url.Values{
			"maxHeight": {strconv.Itoa(maxHeight)},
			"quality":   {"85"},
		})
		if err != nil {
			continue
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header = c.headers(c.token())
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		ct := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if resp.StatusCode == 200 {
			if ct == "" {
				ct = "image/jpeg"
			}
			return data, ct
		}
	}
	return nil, ""
}

// ── Playback ─────────────────────────────────────────────────────────────────

func (c *Client) playbackInfo(itemID string) (map[string]any, error) {
	return c.playbackInfoProfile(itemID, defaultDeviceProfile())
}

// defaultDeviceProfile lets mpv direct-play almost anything (it muxes/decodes
// everything itself), so we advertise the source containers as direct-play.
func defaultDeviceProfile() map[string]any {
	return map[string]any{
		"MaxStreamingBitrate": 200000000,
		"DirectPlayProfiles": []any{
			map[string]any{"Container": "mkv,mp4,mov,avi,ts,m2ts,webm", "Type": "Video"},
			map[string]any{"Container": "mp3,flac,aac,m4a,wav", "Type": "Audio"},
		},
		"TranscodingProfiles": []any{
			map[string]any{"Container": "ts", "Type": "Video", "Protocol": "hls",
				"AudioCodec": "aac,mp3,ac3,eac3", "VideoCodec": "h264,hevc"},
		},
	}
}

func (c *Client) playbackInfoProfile(itemID string, profile map[string]any) (map[string]any, error) {
	body := map[string]any{"DeviceProfile": profile}
	q := url.Values{
		"UserId":              {c.userID()},
		"StartTimeTicks":      {"0"},
		"IsPlayback":          {"true"},
		"AutoOpenLiveStream":  {"true"},
		"MaxStreamingBitrate": {"200000000"},
	}
	data, err := c.request("POST", "/Items/"+itemID+"/PlaybackInfo", q, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

// ResumePositionSeconds reads UserData.PlaybackPositionTicks for an item.
//
// It deliberately ignores a saved position that sits at/near the very end of the
// file. Emby keeps a resume point even when you finished watching, so replaying
// such an item would pass mpv --start=<almost the duration>; mpv then seeks to
// the end, hits EOF on the first frame, and exits immediately — which looks
// exactly like a crash. In that case we start from the beginning.
func (c *Client) ResumePositionSeconds(itemID string) float64 {
	data, err := c.ItemDetailRaw(itemID)
	if err != nil {
		return 0
	}
	var out struct {
		RunTimeTicks int64 `json:"RunTimeTicks"`
		UserData     struct {
			PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
		} `json:"UserData"`
	}
	if json.Unmarshal(data, &out) != nil {
		return 0
	}
	pos := out.UserData.PlaybackPositionTicks
	if pos <= 0 {
		return 0
	}
	// 1 second = 1e7 ticks. Treat "within the last 15s" or "past 99%" of the
	// duration as finished, and restart from 0 instead of resuming at the tail.
	if dur := out.RunTimeTicks; dur > 0 {
		if pos >= dur-15*int64(1e7) || float64(pos)/float64(dur) >= 0.99 {
			return 0
		}
	}
	return float64(pos) / 1e7
}

func (c *Client) absolutizePlayURL(pathOrURL string) string {
	if pathOrURL == "" {
		return ""
	}
	u := pathOrURL
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		if !strings.HasPrefix(u, "/") {
			u = "/" + u
		}
		u = c.serverURL() + u
	}
	if c.token() != "" && !strings.Contains(u, "api_key=") {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "api_key=" + url.QueryEscape(c.token())
	}
	return u
}

// ChoosePlayURL returns the direct-play URL for the chosen media source.
// MediaSourceID can differ from the item id and must be reported to Emby.
func (c *Client) ChoosePlayURL(itemID, requestedSourceID string) (PlayURLChoice, error) {
	info, err := c.playbackInfo(itemID)
	if err != nil {
		return PlayURLChoice{}, err
	}
	sources, _ := info["MediaSources"].([]any)
	if len(sources) == 0 {
		u, err := c.makeURL("/emby/Videos/"+url.PathEscape(itemID)+"/stream", true, url.Values{"Static": {"true"}})
		return PlayURLChoice{URL: u, MediaSourceID: itemID}, err
	}
	source, _ := sources[0].(map[string]any)
	for _, raw := range sources {
		candidate, _ := raw.(map[string]any)
		if requestedSourceID != "" && firstString(candidate, "Id", "MediaSourceId") == requestedSourceID {
			source = candidate
			break
		}
	}
	mediaSourceID := firstString(source, "Id", "MediaSourceId")
	if mediaSourceID == "" {
		mediaSourceID = itemID
	}
	container := normalizeContainer(strs(source["Container"]))
	if direct := strs(source["DirectStreamUrl"]); direct != "" {
		return PlayURLChoice{
			URL:           c.absolutizePlayURL(direct),
			MediaSourceID: mediaSourceID,
		}, nil
	}
	u, err := c.makeURL("/emby/Videos/"+url.PathEscape(itemID)+"/stream."+container, true,
		url.Values{"Static": {"true"}, "MediaSourceId": {mediaSourceID}})
	return PlayURLChoice{
		URL:           u,
		MediaSourceID: mediaSourceID,
	}, err
}

// ── Playback session reporting (fire-and-forget) ─────────────────────────────

func (c *Client) ReportStart(itemID, sessionID, mediaSourceID string) {
	if mediaSourceID == "" {
		mediaSourceID = itemID
	}
	_, _ = c.request("POST", "/Sessions/Playing", nil, map[string]any{
		"ItemId": itemID, "PlaySessionId": sessionID, "MediaSourceId": mediaSourceID,
		"CanSeek": true, "IsPaused": false, "PlayMethod": "DirectPlay", "PositionTicks": 0,
	})
}

func (c *Client) ReportProgress(itemID, sessionID string, posTicks int64, isPaused bool, mediaSourceID string) {
	if mediaSourceID == "" {
		mediaSourceID = itemID
	}
	_, _ = c.request("POST", "/Sessions/Playing/Progress", nil, map[string]any{
		"ItemId": itemID, "PlaySessionId": sessionID, "MediaSourceId": mediaSourceID,
		"PositionTicks": posTicks, "IsPaused": isPaused, "PlayMethod": "DirectPlay",
		"EventName": "TimeUpdate",
	})
}

func (c *Client) ReportStopped(itemID, sessionID string, posTicks int64, mediaSourceID string) {
	if mediaSourceID == "" {
		mediaSourceID = itemID
	}
	_, _ = c.request("POST", "/Sessions/Playing/Stopped", nil, map[string]any{
		"ItemId": itemID, "PlaySessionId": sessionID, "MediaSourceId": mediaSourceID,
		"PositionTicks": posTicks, "PlayMethod": "DirectPlay",
	})
}

func normalizeContainer(container string) string {
	container = strings.ToLower(strings.TrimSpace(strings.Split(container, ",")[0]))
	switch container {
	case "", "matroska":
		return "mkv"
	default:
		return container
	}
}

// ── small helpers ────────────────────────────────────────────────────────────

func strs(v any) string {
	s, _ := v.(string)
	return s
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strs(m[k]); s != "" {
			return s
		}
	}
	return ""
}
