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
	"sort"
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

// Artwork requests can fan out across an entire library grid. Keep them from
// holding the mobile UI for the longer API/playback timeout.
var imageHTTPClient = &http.Client{Timeout: 5 * time.Second}

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

// New builds a client for an explicit server (used by Authenticate and Probe).
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

// headersFor builds the request headers for one credential channel.
//
// The default sends both spellings of the auth header. Emby reads
// X-Emby-Authorization, Jellyfin canonicalised on Authorization, and each
// ignores the other — so sending both is a strict superset that removes the
// declared source type from the auth path entirely. The narrower styles exist
// only for the ladder in request(), which runs after a token was rejected.
func (c *Client) headersFor(style authStyle, token string) http.Header {
	h := http.Header{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json; charset=utf-8"},
	}
	value := c.authHeaderValue(token)
	switch style {
	case authEmbyOnly:
		h.Set("X-Emby-Authorization", value)
	case authCanonical:
		h.Set("Authorization", value)
	case authQuery:
		// The credential rides in the query string instead; see makeURL.
	default:
		h.Set("Authorization", value)
		h.Set("X-Emby-Authorization", value)
	}
	return h
}

// escalationExhausted reports whether the whole credential ladder was tried and
// every channel was rejected. Only then is it safe to stop escalating: a
// network failure or a 404 proves nothing about credentials, and pinning the
// channel on that evidence would disable the ladder for good.
func escalationExhausted(styles []authStyle, lastRejected bool) bool {
	return len(styles) > 1 && lastRejected
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

// do performs exactly one attempt and returns the raw body plus status.
func (c *Client) do(method, path string, params url.Values, body any, style authStyle) ([]byte, int, error) {
	u, err := c.makeURL(path, style == authQuery, params)
	if err != nil {
		return nil, 0, err
	}
	var reader io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header = c.headersFor(style, c.token())
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return data, resp.StatusCode, nil
}

// request performs the call across the learned dialect's candidates and
// returns the raw response body so proxy handlers can pass it straight through.
//
// Two ladders, both of which collapse to a single attempt once the server is
// understood: the API mount point (bare vs /emby) and — only after a token is
// actually rejected — the credential channel. Failures are classified rather
// than counted, so a 401 does not send us hunting for another path and a 200
// carrying a proxy's HTML never reaches the caller as success.
func (c *Client) request(method, path string, params url.Values, body any) ([]byte, error) {
	c.reload()
	if c.serverURL() == "" {
		return nil, errf(400, "No Emby server is configured")
	}

	d := dialectFor(c.server)
	prefixes := prefixCandidates(c.basePath(), d.prefix, d.prefixKnown, c.bareFirstHint())
	styles := authStylesFor(d.auth, d.authSettled(c.token()), c.token() != "")

	var lastErr error = errf(500, "Emby request failed")
	rejected := false
	for _, style := range styles {
		rejected = false
		for _, prefix := range prefixes {
			full := joinPrefix(prefix, path)
			data, status, err := c.do(method, full, params, body, style)
			if err != nil {
				lastErr = err
				continue
			}
			switch classify(status, data) {
			case outcomeSuccess:
				c.learnPrefix(prefix)
				if token := c.token(); token != "" {
					c.learnAuth(style, token)
				}
				return data, nil
			case outcomeUnauthorized:
				rejected = true
				lastErr = errf(401, "Authentication failed. Please sign in again.")
			case outcomeMissingRoute:
				lastErr = errf(404, "Endpoint not found: %s", full)
			case outcomeBadShape:
				lastErr = errf(502, "%s answered with a non-JSON body; a reverse proxy is probably intercepting the API path", full)
			default:
				lastErr = errf(status, "Emby returned HTTP %d", status)
			}
		}
		if !rejected {
			break // the credential channel is not what is failing
		}
	}
	if escalationExhausted(styles, rejected) {
		// Every channel was rejected, so this token is bad rather than the
		// transport. Stop paying for the ladder — but pin the verdict to the
		// token itself, so the next credential reopens it whichever code path
		// stored it.
		fingerprint := tokenFingerprint(c.token())
		updateDialect(c.server, func(d *dialect) {
			d.authKnown, d.authToken = true, fingerprint
		})
	}
	return nil, lastErr
}

// ── Auth ─────────────────────────────────────────────────────────────────────

// Authenticate verifies credentials against srv and returns (accessToken,
// userID) WITHOUT persisting anything. Storing the result is the caller's job:
// the sign-in handler persists immediately, while the "add server" flow only
// persists once this succeeds, so a wrong address/password never leaves a
// broken entry behind.
func Authenticate(srv *config.Server, username, password string) (string, string, error) {
	c := &Client{server: srv}
	if c.serverURL() == "" {
		return "", "", errf(400, "No server address is configured")
	}

	lastErr := "Login failed"
	// Sign-in carries no token, so only the mount-point ladder applies here.
	// Both header spellings go out at once (see headersFor).
	for _, prefix := range c.prefixes() {
		path := joinPrefix(prefix, "/Users/AuthenticateByName")
		buf, _ := json.Marshal(map[string]string{"Username": username, "Pw": password})
		req, err := http.NewRequest("POST", c.serverURL()+path, bytes.NewReader(buf))
		if err != nil {
			lastErr = err.Error()
			continue
		}
		req.Header = c.headersFor(authBoth, "")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch classify(resp.StatusCode, data) {
		case outcomeUnauthorized:
			lastErr = "Incorrect username or password"
			continue
		case outcomeMissingRoute:
			lastErr = "No Emby login endpoint found"
			continue
		case outcomeBadShape:
			lastErr = "the address answered with a non-JSON body; check for a reverse proxy in front of the API"
			continue
		case outcomeServerError:
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
		c.learnPrefix(prefix)
		return out.AccessToken, out.User.ID, nil
	}
	return "", "", errf(400, "Login failed: %s", lastErr)
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

func (c *Client) Episodes(seriesID, seasonID string, start, limit int, order string) ([]byte, error) {
	if limit < 1 {
		limit = 1
	}
	if start < 0 {
		start = 0
	}
	// Do NOT trust the server's ordering: Emby's /Shows/{id}/Episodes ignores
	// SortBy/SortOrder and floats watched episodes to the bottom of the list
	// (verified against a real 4.x server), and other servers/versions have their
	// own quirks. Fetch the whole season and order it ourselves, mirroring
	// plex.Client.Episodes. Episode counts per series are bounded (One Piece
	// ~1100), so one fetch + in-memory sort + slice is safe and deterministic.
	q := url.Values{
		"UserId": {c.userID()},
		"Fields": {"Overview,RunTimeTicks,UserData,SeriesName,SeasonName,IndexNumber,ParentIndexNumber"},
		"Limit":  {"5000"},
	}
	if seasonID != "" {
		q.Set("SeasonId", seasonID)
	}
	data, err := c.request("GET", "/Shows/"+seriesID+"/Episodes", q, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Items []map[string]any `json:"Items"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return data, nil // unexpected shape: fall back to the raw payload
	}
	items := parsed.Items
	key := func(m map[string]any) int {
		return episodeInt(m["ParentIndexNumber"])*100000 + episodeInt(m["IndexNumber"])
	}
	sort.Slice(items, func(i, j int) bool {
		if order == "desc" {
			return key(items[i]) > key(items[j])
		}
		return key(items[i]) < key(items[j])
	})
	total := len(items)
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return json.Marshal(map[string]any{
		"Items":            items[start:end],
		"TotalRecordCount": total,
	})
}

// episodeInt coerces a decoded JSON value (numbers arrive as float64) to an int
// for episode/season ordering; anything unrecognized sorts as 0.
func episodeInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
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
	bare := []string{}
	if index >= 0 {
		bare = append(bare, "/Items/"+itemID+"/Images/"+imageType+"/"+strconv.Itoa(index))
		if index == 0 {
			bare = append(bare, "/Items/"+itemID+"/Images/"+imageType)
		}
	} else {
		bare = append(bare, "/Items/"+itemID+"/Images/"+imageType)
	}
	prefixes := c.prefixes()
	style := dialectFor(c.server).auth
	paths := make([]string, 0, len(bare)*len(prefixes))
	for _, path := range bare {
		for _, prefix := range prefixes {
			paths = append(paths, joinPrefix(prefix, path))
		}
	}
	for _, path := range paths {
		u, err := c.makeURL(path, style == authQuery, url.Values{
			"maxHeight": {strconv.Itoa(maxHeight)},
			"quality":   {"85"},
		})
		if err != nil {
			continue
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header = c.headersFor(style, c.token())
		resp, err := imageHTTPClient.Do(req)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		ct := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil || len(data) == 0 {
			continue
		}
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
		// mpv cannot retry a second path candidate, so this URL uses the mount
		// point learned from the API calls that got us here.
		u, err := c.makeURL(c.preferredPath("/Videos/"+url.PathEscape(itemID)+"/stream"), true, url.Values{"Static": {"true"}})
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
	u, err := c.makeURL(c.preferredPath("/Videos/"+url.PathEscape(itemID)+"/stream."+container), true,
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
