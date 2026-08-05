package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"tvremote/internal/imagecache"
	"tvremote/internal/provider"
)

// mediaClient resolves the request's source to a poster-wall provider
// (Emby/Jellyfin/Plex), honoring ?server_id= before the active source.
func mediaClient(r *http.Request) (provider.Media, error) {
	return clientForRequest(r, provider.FromServer, provider.Active)
}

func (s *Server) embyLibraries(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.Libraries()
	writeRaw(w, r, body, err)
}

func (s *Server) embyResume(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.Resume(qInt(r, "start", 0), qInt(r, "limit", 12))
	writeRaw(w, r, body, err)
}

func (s *Server) embyItems(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	search := r.URL.Query().Get("search")
	body, err := c.Items(
		r.URL.Query().Get("parent_id"),
		search,
		qInt(r, "start", 0),
		qInt(r, "limit", 60),
		search != "",
	)
	if err == nil && search != "" {
		body = normalizeSearchItems(c, body, search)
	}
	writeRaw(w, r, body, err)
}

// normalizeSearchItems collapses noisy flat search results into user-facing
// entities. A series-title match becomes one Series card; episodes stay only
// when the episode title itself matches.
func normalizeSearchItems(c provider.Media, body []byte, search string) []byte {
	var payload struct {
		Items            []map[string]any `json:"Items"`
		TotalRecordCount int              `json:"TotalRecordCount"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	needle := normalizedSearchText(search)
	knownSeries := map[string]bool{}
	for _, item := range payload.Items {
		item["Type"] = canonicalItemType(itemString(item, "Type"))
		if itemString(item, "Type") == "Series" {
			if id := itemString(item, "Id"); id != "" {
				knownSeries[id] = true
			}
		}
	}

	parentIDs := map[string]bool{}
	for _, item := range payload.Items {
		if itemString(item, "Type") != "Episode" {
			continue
		}
		seriesID := itemString(item, "SeriesId")
		if seriesID != "" && !knownSeries[seriesID] &&
			strings.Contains(normalizedSearchText(itemString(item, "SeriesName")), needle) {
			parentIDs[seriesID] = true
		}
	}
	ids := make([]string, 0, len(parentIDs))
	for id := range parentIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if parent, ok := fetchSearchParentSeries(c, id); ok {
			payload.Items = append(payload.Items, parent)
			knownSeries[id] = true
		}
	}

	filtered := payload.Items[:0]
	seen := map[string]bool{}
	for _, item := range payload.Items {
		typ := itemString(item, "Type")
		if typ == "Episode" {
			seriesTitleMatches := strings.Contains(normalizedSearchText(itemString(item, "SeriesName")), needle)
			episodeTitleMatches := strings.Contains(normalizedSearchText(itemString(item, "Name")), needle)
			if seriesTitleMatches || !episodeTitleMatches {
				continue
			}
		}
		key := typ + ":" + itemString(item, "Id")
		if seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, item)
	}
	payload.Items = filtered
	payload.TotalRecordCount = len(filtered)
	if out, err := json.Marshal(payload); err == nil {
		return out
	}
	return body
}

func fetchSearchParentSeries(c provider.Media, id string) (map[string]any, bool) {
	body, err := c.ItemDetailRaw(id)
	if err != nil {
		return nil, false
	}
	var parent map[string]any
	if json.Unmarshal(body, &parent) != nil || itemString(parent, "Id") == "" {
		return nil, false
	}
	parent["Type"] = "Series"
	return parent, true
}

func itemString(item map[string]any, key string) string {
	if s, ok := item[key].(string); ok {
		return s
	}
	return ""
}

func normalizedSearchText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func canonicalItemType(value string) string {
	switch strings.ToLower(value) {
	case "movie":
		return "Movie"
	case "series", "show":
		return "Series"
	case "season":
		return "Season"
	case "episode":
		return "Episode"
	case "boxset", "collection":
		return "BoxSet"
	case "musicvideo":
		return "MusicVideo"
	default:
		return "Video"
	}
}

func (s *Server) embyItemDetail(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.ItemDetailRaw(r.PathValue("item_id"))
	if err == nil {
		body = addPlaybackVariants(body)
	}
	writeRaw(w, r, body, err)
}

func addPlaybackVariants(body []byte) []byte {
	var item map[string]any
	if json.Unmarshal(body, &item) != nil {
		return body
	}
	sources, _ := item["MediaSources"].([]any)
	variants := []map[string]any{}
	for i, raw := range sources {
		source, _ := raw.(map[string]any)
		id, _ := source["Id"].(string)
		if id == "" {
			id, _ = source["MediaSourceId"].(string)
		}
		if id == "" {
			continue
		}
		v := map[string]any{"id": id, "name": source["Name"], "container": source["Container"], "bitrate": source["Bitrate"], "size": source["Size"], "is_default": i == 0}
		if streams, ok := source["MediaStreams"].([]any); ok {
			for _, sr := range streams {
				stream, _ := sr.(map[string]any)
				typ, _ := stream["Type"].(string)
				if strings.EqualFold(typ, "video") {
					v["width"], v["height"], v["video_codec"] = stream["Width"], stream["Height"], stream["Codec"]
					v["video_range"] = stream["VideoRange"]
					break
				}
			}
		}
		variants = append(variants, v)
	}
	item["PlaybackVariants"] = variants
	out, err := json.Marshal(item)
	if err != nil {
		return body
	}
	return out
}

func (s *Server) embyEpisodes(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "asc"
	}
	body, err := c.Episodes(
		r.URL.Query().Get("series_id"),
		r.URL.Query().Get("season_id"),
		qInt(r, "start", 0),
		qInt(r, "limit", 100),
		sort,
	)
	writeRaw(w, r, body, err)
}

func (s *Server) embySeasons(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.Seasons(r.URL.Query().Get("series_id"))
	writeRaw(w, r, body, err)
}

// imageCacheControl lets a phone reuse artwork it already holds for as long as
// the disk cache considers it fresh, then fall back to a conditional request
// rather than a download. stale-while-revalidate covers the gap between the two
// windows: a browser that has it repaints from its own cache and revalidates in
// the background, and one that does not sends a conditional request that this
// server answers from disk without touching the media server.
var imageCacheControl = fmt.Sprintf("private, max-age=%d, stale-while-revalidate=%d",
	int(imagecache.FreshFor.Seconds()), int(imagecache.HardTTL.Seconds()))

func (s *Server) embyImage(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	itemID := r.PathValue("item_id")
	imageType := r.URL.Query().Get("type")
	maxHeight := qInt(r, "max_height", 400)
	entry, ok := imagecache.Fetch(imagecache.Key{
		ServerID:  requestServerID(r),
		ItemID:    itemID,
		Type:      imageType,
		MaxHeight: maxHeight,
	}, func() ([]byte, string) {
		return c.ImageBytes(itemID, maxHeight, imageType)
	})
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	etag := `"` + entry.ETag + `"`
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", imageCacheControl)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(entry.Data)
}

// etagMatches reports whether a client's If-None-Match covers etag. The header
// is a comma-separated list, entries may carry the weak prefix, and "*" matches
// anything; weak and strong compare alike here because the only representation
// of a given digest is byte-identical.
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
