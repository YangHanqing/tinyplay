package server

import (
	"net/http"
	"sort"
	"strings"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/iptv"
)

// iptvClient resolves the request's source to an IPTV client, honoring
// ?server_id= before the active source so the common case (browsing the
// currently-selected IPTV source) needs no query param at all.
func iptvClient(r *http.Request) (*iptv.Client, error) {
	return clientForRequest(r, iptv.FromServer, iptv.FromActive)
}

func (s *Server) iptvSummary(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c.Summary())
}

func (s *Server) iptvRefresh(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := c.Refresh(r.Context()); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c.Summary())
}

func (s *Server) iptvCategories(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := append([]string{"all", "favorites", "recent"}, c.Categories()...)
	writeJSON(w, http.StatusOK, out)
}

func iptvChannelRow(ch iptv.Channel, favorites map[string]bool, current *iptv.Programme) map[string]any {
	row := map[string]any{
		"id":            ch.ID,
		"name":          ch.Name,
		"logo_url":      ch.LogoURL,
		"group_title":   ch.GroupTitle,
		"quality":       ch.Quality,
		"variant_count": len(ch.Variants),
		"is_favorite":   favorites[ch.ID],
		"has_epg":       false,
		"can_catchup":   ch.CatchupSource != "" && ch.CatchupDays > 0,
	}
	if current != nil {
		row["has_epg"] = true
		row["current_programme"] = map[string]any{"title": current.Title}
	} else {
		row["current_programme"] = nil
	}
	return row
}

// iptvPublicVariants deliberately omits raw stream URLs and header values.
// The browser only needs labels to select a variant; resolving its private URL
// remains inside the backend's play handler.
func iptvPublicVariants(ch iptv.Channel) []map[string]any {
	out := make([]map[string]any, 0, len(ch.Variants))
	for _, variant := range ch.Variants {
		out = append(out, map[string]any{
			"label":            variant.Label,
			"has_http_headers": len(variant.HTTPHeaders) > 0,
		})
	}
	return out
}

func (s *Server) iptvChannels(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if serverID == "" {
		if active := config.ActiveServer(); active != nil {
			serverID = active.ID
		}
	}
	category := r.URL.Query().Get("category")
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	favorites := map[string]bool{}
	for _, id := range config.IPTVFavorites(serverID) {
		favorites[id] = true
	}
	recent := map[string]int{}
	for i, rc := range config.IPTVRecent(serverID) {
		recent[rc.ChannelID] = i
	}

	channels := []iptv.Channel{}
	for _, ch := range c.Channels() {
		switch category {
		case "", "all":
		case "favorites":
			if !favorites[ch.ID] {
				continue
			}
		case "recent":
			if _, ok := recent[ch.ID]; !ok {
				continue
			}
		default:
			if ch.GroupTitle != category {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(ch.Name), search) {
			continue
		}
		channels = append(channels, ch)
	}
	currentByChannel := c.CurrentProgrammes(channels)
	out := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		var current *iptv.Programme
		if programme, ok := currentByChannel[ch.ID]; ok {
			current = &programme
		}
		out = append(out, iptvChannelRow(ch, favorites, current))
	}
	if category == "recent" {
		sort.Slice(out, func(i, j int) bool {
			return recent[out[i]["id"].(string)] < recent[out[j]["id"].(string)]
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) iptvChannelDetail(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ch := c.ChannelByID(r.PathValue("id"))
	if ch == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "channel_not_found")})
		return
	}
	serverID := c.ServerID()
	favorites := map[string]bool{}
	for _, id := range config.IPTVFavorites(serverID) {
		favorites[id] = true
	}
	row := iptvChannelRow(*ch, favorites, c.CurrentProgramme(ch.ID))
	row["tvg_id"] = ch.TvgID
	row["variants"] = iptvPublicVariants(*ch)
	row["catchup_days"] = ch.CatchupDays
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) iptvProgramme(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	channelID := r.URL.Query().Get("channel_id")
	count := qInt(r, "count", 4)
	if count < 1 {
		count = 1
	}
	if count > 48 {
		count = 48
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current":  c.CurrentProgramme(channelID),
		"past":     c.PastProgrammes(channelID, count),
		"upcoming": c.UpcomingProgrammes(channelID, count),
	})
}

// iptvBodyServerID resolves a request body's server_id, falling back to the
// active server so callers (the phone web frontend) don't need to track
// the active source id just to record a favorite/recent-watch.
func iptvBodyServerID(serverID string) string {
	if serverID != "" {
		return serverID
	}
	if active := config.ActiveServer(); active != nil {
		return active.ID
	}
	return ""
}

func (s *Server) iptvFavorite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID  string `json:"server_id"`
		ChannelID string `json:"channel_id"`
		Favorite  *bool  `json:"favorite"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	serverID := iptvBodyServerID(body.ServerID)
	if serverID == "" || body.ChannelID == "" {
		invalidBody(w, r)
		return
	}
	favorites := config.IPTVFavorites(serverID)
	isFavorite := false
	for _, id := range favorites {
		if id == body.ChannelID {
			isFavorite = true
			break
		}
	}
	// Toggle records add/remove; an explicit favorite:false/true short-circuits
	// when it already matches the current state, keeping the call idempotent.
	if body.Favorite == nil || *body.Favorite != isFavorite {
		favorites = config.ToggleIPTVFavorite(serverID, body.ChannelID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorites": favorites})
}

func (s *Server) iptvRecent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID  string `json:"server_id"`
		ChannelID string `json:"channel_id"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	serverID := iptvBodyServerID(body.ServerID)
	if serverID == "" || body.ChannelID == "" {
		invalidBody(w, r)
		return
	}
	recent := config.RecordIPTVRecent(serverID, body.ChannelID, 50)
	writeJSON(w, http.StatusOK, map[string]any{"recently_watched": recent})
}
