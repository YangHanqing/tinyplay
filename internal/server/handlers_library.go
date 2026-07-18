package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

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

func (s *Server) embyImage(w http.ResponseWriter, r *http.Request) {
	c, err := mediaClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	imageType := r.URL.Query().Get("type")
	data, ct := c.ImageBytes(r.PathValue("item_id"), qInt(r, "max_height", 400), imageType)
	if data == nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
