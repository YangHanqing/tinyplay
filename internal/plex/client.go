// Package plex adapts Plex's API to the Emby-shaped JSON consumed by TinyPlay's
// shared phone frontend.
package plex

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

const ticksPerMillisecond int64 = 10_000

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string   { return e.Msg }
func (e *APIError) StatusCode() int { return e.Status }
func errf(status int, format string, args ...any) *APIError {
	return &APIError{status, fmt.Sprintf(format, args...)}
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

type Client struct{ server *config.Server }
type PlayURLChoice struct{ URL, MediaSourceID string }

func New(server *config.Server) *Client { return &Client{server: server} }
func FromActive() (*Client, error) {
	srv := config.ActiveServer()
	if srv == nil || config.NormalizeServerType(srv.Type) != "plex" {
		return nil, errf(400, "No Plex server is available")
	}
	return New(srv), nil
}

func (c *Client) baseURL() string { return config.BuildServerURL(c.server) }
func (c *Client) token() string {
	if c.server == nil {
		return ""
	}
	return c.server.AccessToken
}
func (c *Client) deviceID() string {
	if c.server != nil && c.server.DeviceID != "" {
		return c.server.DeviceID
	}
	return "tv-remote-mpv-001"
}
func (c *Client) headers(token string) http.Header {
	if token == "" {
		token = c.token()
	}
	h := http.Header{
		"Accept": {"application/json"}, "X-Plex-Client-Identifier": {c.deviceID()},
		"X-Plex-Product": {"TinyPlay"}, "X-Plex-Version": {"4.7.0.0"},
		"X-Plex-Platform": {"Desktop"}, "X-Plex-Device-Name": {"TinyPlay"},
	}
	if token != "" {
		h.Set("X-Plex-Token", token)
	}
	return h
}

func (c *Client) get(path string, values url.Values) (map[string]any, error) {
	if c.baseURL() == "" {
		return nil, errf(400, "No server address is configured")
	}
	u := c.baseURL() + path
	if len(values) > 0 {
		u += "?" + values.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header = c.headers("")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errf(502, "Plex request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errf(401, "Authentication failed. Please sign in again.")
	}
	if resp.StatusCode == 404 {
		return nil, errf(404, "Endpoint not found: %s", path)
	}
	if resp.StatusCode >= 400 {
		return nil, errf(resp.StatusCode, "Plex returned HTTP %d", resp.StatusCode)
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if json.Unmarshal(body, &out) != nil {
		return nil, errf(502, "Plex returned invalid JSON")
	}
	return out, nil
}

func Authenticate(server *config.Server, username, password, token string) (string, string, error) {
	c := New(server)
	if token != "" {
		req, _ := http.NewRequest("GET", c.baseURL()+"/library/sections", nil)
		req.Header = c.headers(token)
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", "", errf(400, "Login failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return "", "", errf(401, "Incorrect username or password")
		}
		if resp.StatusCode >= 400 {
			return "", "", errf(400, "Login failed: HTTP %d", resp.StatusCode)
		}
		return token, "", nil
	}
	form := url.Values{"login": {username}, "password": {password}}
	req, _ := http.NewRequest("POST", "https://plex.tv/api/v2/users/signin", bytes.NewBufferString(form.Encode()))
	req.Header = c.headers("")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", errf(400, "Login failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", "", errf(401, "Incorrect username or password")
	}
	if resp.StatusCode >= 400 {
		return "", "", errf(400, "Login failed: HTTP %d", resp.StatusCode)
	}
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	auth := stringValue(data["authToken"])
	if auth == "" {
		auth = stringValue(data["authtoken"])
	}
	if auth == "" {
		return "", "", errf(400, "Login succeeded but AccessToken/User.Id was missing")
	}
	return auth, "", nil
}

func container(data map[string]any) map[string]any {
	m, _ := data["MediaContainer"].(map[string]any)
	return m
}
func maps(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func metadata(data map[string]any) []map[string]any { return maps(container(data)["Metadata"]) }
func integer(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	case int64:
		return x
	case int:
		return int64(x)
	}
	return 0
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		n, e := strconv.ParseFloat(x, 64)
		return n, e == nil
	}
	return 0, false
}
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

var typeMap = map[string]string{"movie": "Movie", "show": "Series", "season": "Season", "episode": "Episode", "clip": "Video", "artist": "MusicArtist", "album": "MusicAlbum", "track": "Audio"}

func mapItem(m map[string]any, detail bool) map[string]any {
	typ := strings.ToLower(stringValue(m["type"]))
	dur := integer(m["duration"])
	off := integer(m["viewOffset"])
	count := integer(m["viewCount"])
	pct := float64(0)
	if dur > 0 && off > 0 {
		pct = float64(off) * 100 / float64(dur)
	} else if count > 0 {
		pct = 100
	}
	out := map[string]any{"Id": stringValue(m["ratingKey"]), "Name": stringValue(m["title"]), "Type": typeMap[typ], "Overview": stringValue(m["summary"]), "RunTimeTicks": dur * ticksPerMillisecond,
		"UserData": map[string]any{"PlaybackPositionTicks": off * ticksPerMillisecond, "PlayedPercentage": pct, "PlayCount": count, "Played": count > 0 && off == 0}}
	if out["Type"] == "" {
		out["Type"] = "Video"
	}
	for src, dst := range map[string]string{"year": "ProductionYear", "index": "IndexNumber", "parentIndex": "ParentIndexNumber", "leafCount": "RecursiveItemCount", "childCount": "ChildCount"} {
		if _, ok := m[src]; ok {
			out[dst] = integer(m[src])
		}
	}
	for src, dst := range map[string]string{"grandparentRatingKey": "SeriesId", "parentRatingKey": "SeasonId", "grandparentTitle": "SeriesName", "parentTitle": "SeasonName"} {
		if v := stringValue(m[src]); v != "" && v != "<nil>" {
			out[dst] = v
		}
	}
	if rating, ok := number(m["rating"]); ok {
		out["CommunityRating"] = rating
	}
	if detail {
		genres := []string{}
		for _, g := range maps(m["Genre"]) {
			if s := stringValue(g["tag"]); s != "" {
				genres = append(genres, s)
			}
		}
		if len(genres) > 0 {
			out["Genres"] = genres
		}
		sources := []map[string]any{}
		for _, media := range maps(m["Media"]) {
			for _, part := range maps(media["Part"]) {
				sources = append(sources, map[string]any{
					"Id": stringValue(part["id"]), "Name": stringValue(media["videoResolution"]),
					"Container": stringValue(media["container"]), "Bitrate": integer(media["bitrate"]),
					"MediaStreams": []any{map[string]any{"Type": "Video", "Width": integer(media["width"]), "Height": integer(media["height"]), "Codec": stringValue(media["videoCodec"])}},
				})
			}
		}
		out["MediaSources"] = sources
	}
	return out
}
func marshalItems(items []map[string]any, total int64) ([]byte, error) {
	return json.Marshal(map[string]any{"Items": items, "TotalRecordCount": total})
}

func (c *Client) Libraries() ([]byte, error) {
	d, err := c.get("/library/sections", nil)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	kinds := map[string]string{"movie": "movies", "show": "tvshows", "artist": "music", "photo": "homevideos"}
	for _, m := range maps(container(d)["Directory"]) {
		out = append(out, map[string]any{"Id": stringValue(m["key"]), "Name": stringValue(m["title"]), "CollectionType": kinds[strings.ToLower(stringValue(m["type"]))]})
	}
	return marshalItems(out, int64(len(out)))
}
func (c *Client) Items(parent, search string, start, limit int, includeEpisodes bool) ([]byte, error) {
	if search != "" {
		d, e := c.get("/search", url.Values{"query": {search}, "limit": {strconv.Itoa(limit)}})
		if e != nil {
			return nil, e
		}
		ms := metadata(d)
		out := make([]map[string]any, 0, len(ms))
		for _, m := range ms {
			out = append(out, mapItem(m, false))
		}
		return marshalItems(out, int64(len(out)))
	}
	if parent == "" {
		return marshalItems([]map[string]any{}, 0)
	}
	d, e := c.get("/library/sections/"+url.PathEscape(parent)+"/all", url.Values{"X-Plex-Container-Start": {strconv.Itoa(start)}, "X-Plex-Container-Size": {strconv.Itoa(limit)}})
	if e != nil {
		return nil, e
	}
	ms := metadata(d)
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, mapItem(m, false))
	}
	total := integer(container(d)["totalSize"])
	if total == 0 {
		total = integer(container(d)["size"])
	}
	if total == 0 {
		total = int64(len(out))
	}
	return marshalItems(out, total)
}
func (c *Client) Resume(start, limit int) ([]byte, error) {
	d, e := c.get("/library/onDeck", url.Values{"X-Plex-Container-Start": {strconv.Itoa(start)}, "X-Plex-Container-Size": {strconv.Itoa(limit)}})
	if e != nil {
		return nil, e
	}
	ms := metadata(d)
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, mapItem(m, false))
	}
	return marshalItems(out, int64(len(out)))
}
func (c *Client) ItemDetailRaw(id string) ([]byte, error) {
	d, e := c.get("/library/metadata/"+url.PathEscape(id), nil)
	if e != nil {
		return nil, e
	}
	ms := metadata(d)
	if len(ms) == 0 {
		return nil, errf(404, "Item not found")
	}
	return json.Marshal(mapItem(ms[0], true))
}
func (c *Client) Episodes(series, season string, start, limit int, order string) ([]byte, error) {
	path := "/library/metadata/" + url.PathEscape(series) + "/allLeaves"
	if season != "" {
		path = "/library/metadata/" + url.PathEscape(season) + "/children"
	}
	d, e := c.get(path, nil)
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, m := range metadata(d) {
		if strings.EqualFold(stringValue(m["type"]), "episode") {
			out = append(out, mapItem(m, false))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a := integer(out[i]["ParentIndexNumber"])*100000 + integer(out[i]["IndexNumber"])
		b := integer(out[j]["ParentIndexNumber"])*100000 + integer(out[j]["IndexNumber"])
		if order == "desc" {
			return a > b
		}
		return a < b
	})
	total := len(out)
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return marshalItems(out[start:end], int64(total))
}

func (c *Client) Seasons(seriesID string) ([]byte, error) {
	d, e := c.get("/library/metadata/"+url.PathEscape(seriesID)+"/children", nil)
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, m := range metadata(d) {
		if strings.EqualFold(stringValue(m["type"]), "season") {
			out = append(out, mapItem(m, false))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return integer(out[i]["IndexNumber"]) < integer(out[j]["IndexNumber"])
	})
	return marshalItems(out, int64(len(out)))
}

func (c *Client) ImageBytes(id string, maxHeight int, imageType string) ([]byte, string) {
	d, e := c.get("/library/metadata/"+url.PathEscape(id), nil)
	if e != nil {
		return nil, ""
	}
	ms := metadata(d)
	if len(ms) == 0 {
		return nil, ""
	}
	m := ms[0]
	keys := []string{"thumb", "grandparentThumb", "art"}
	if imageType == "Backdrop" {
		keys = []string{"art", "thumb"}
	}
	for _, key := range keys {
		rel := stringValue(m[key])
		if rel == "" || rel == "<nil>" {
			continue
		}
		q := url.Values{"height": {strconv.Itoa(maxHeight)}, "width": {strconv.Itoa(maxHeight)}, "minSize": {"1"}, "upscale": {"1"}, "url": {rel}, "X-Plex-Token": {c.token()}}
		u := c.baseURL() + "/photo/:/transcode?" + q.Encode()
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Accept", "image/*")
		if resp, err := httpClient.Do(req); err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == 200 && len(body) > 0 {
				ct := resp.Header.Get("Content-Type")
				if ct == "" {
					ct = "image/jpeg"
				}
				return body, ct
			}
		}
	}
	return nil, ""
}
func (c *Client) BackdropBytes(id string, maxHeight, index int) ([]byte, string) {
	return c.ImageBytes(id, maxHeight, "Backdrop")
}
func (c *Client) ChoosePlayURL(id, requestedSourceID string) (PlayURLChoice, error) {
	d, e := c.get("/library/metadata/"+url.PathEscape(id), nil)
	if e != nil {
		return PlayURLChoice{}, e
	}
	ms := metadata(d)
	if len(ms) == 0 {
		return PlayURLChoice{}, errf(404, "No playable source")
	}
	media := maps(ms[0]["Media"])
	if len(media) == 0 {
		return PlayURLChoice{}, errf(404, "No playable source")
	}
	parts := maps(media[0]["Part"])
	if requestedSourceID != "" {
		for _, candidate := range media {
			for _, part := range maps(candidate["Part"]) {
				if stringValue(part["id"]) == requestedSourceID {
					parts = []map[string]any{part}
					break
				}
			}
		}
	}
	if len(parts) == 0 {
		return PlayURLChoice{}, errf(404, "No playable source")
	}
	key := stringValue(parts[0]["key"])
	if key == "" {
		return PlayURLChoice{}, errf(404, "No playable source")
	}
	q := "?"
	if strings.Contains(key, "?") {
		q = "&"
	}
	return PlayURLChoice{c.baseURL() + key + q + "X-Plex-Token=" + url.QueryEscape(c.token()), stringValue(parts[0]["id"])}, nil
}
func (c *Client) ResumePositionSeconds(id string) float64 {
	d, e := c.get("/library/metadata/"+url.PathEscape(id), nil)
	if e != nil {
		return 0
	}
	ms := metadata(d)
	if len(ms) == 0 {
		return 0
	}
	off, dur := integer(ms[0]["viewOffset"]), integer(ms[0]["duration"])
	if off <= 0 || (dur > 0 && (off >= dur-15000 || float64(off)/float64(dur) >= .99)) {
		return 0
	}
	return float64(off) / 1000
}
func (c *Client) timeline(id, state string, ticks int64) {
	_, _ = c.get("/:/timeline", url.Values{"ratingKey": {id}, "key": {"/library/metadata/" + id}, "state": {state}, "time": {strconv.FormatInt(max64(0, ticks/ticksPerMillisecond), 10)}, "hasMDE": {"1"}})
}
func (c *Client) ReportStart(id, session, source string) { c.timeline(id, "playing", 0) }
func (c *Client) ReportProgress(id, session string, ticks int64, paused bool, source string) {
	state := "playing"
	if paused {
		state = "paused"
	}
	c.timeline(id, state, ticks)
}
func (c *Client) ReportStopped(id, session string, ticks int64, source string) {
	c.timeline(id, "stopped", ticks)
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
