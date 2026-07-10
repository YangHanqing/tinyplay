package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"tvremote/internal/config"
	"tvremote/internal/filesource"
	"tvremote/internal/i18n"
	"tvremote/internal/iptv"
	"tvremote/internal/player"
	"tvremote/internal/provider"
	"tvremote/internal/sysvolume"
)

// ── response helpers ─────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRaw(w http.ResponseWriter, r *http.Request, body []byte, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	lang := i18n.RequestLang(r)
	var apiErr interface{ StatusCode() int }
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.StatusCode(), map[string]string{"detail": i18n.LocalizeError(lang, err.Error())})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": i18n.LocalizeError(lang, err.Error())})
}

func decode(r *http.Request, v any) bool {
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func qInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// safeServer strips sensitive fields before sending to the frontend.
func safeServer(s *config.Server) map[string]any {
	return map[string]any{
		"id":              s.ID,
		"name":            s.Name,
		"protocol":        s.Protocol,
		"hosts":           s.Hosts,
		"port":            s.Port,
		"active_host":     s.ActiveHost,
		"username":        s.Username,
		"user_id":         s.UserID,
		"last_library_id": s.LastLibraryID,
		"type":            config.NormalizeServerType(s.Type),
		"share":           s.Share,
		"domain":          s.Domain,
		"root_path":       s.RootPath,
		"playlist_url":    s.PlaylistURL,
		"epg_url":         s.EPGURL,
	}
}

// ── App settings ────────────────────────────────────────────────────────────

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, config.Settings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MpvCacheSecs        *int    `json:"mpv_cache_secs"`
		Language            *string `json:"language"`
		SeekBackwardSecs    *int    `json:"seek_backward_secs"`
		SeekForwardSecs     *int    `json:"seek_forward_secs"`
		DLNAReceiverEnabled *bool   `json:"dlna_receiver_enabled"`
	}
	if !decode(r, &body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	if body.MpvCacheSecs != nil {
		config.SetMpvCacheSecs(*body.MpvCacheSecs)
	}
	if body.Language != nil {
		config.SetLanguage(*body.Language)
	}
	if body.SeekBackwardSecs != nil || body.SeekForwardSecs != nil {
		current := config.Settings()
		back := current["seek_backward_secs"].(int)
		forward := current["seek_forward_secs"].(int)
		if body.SeekBackwardSecs != nil {
			back = *body.SeekBackwardSecs
		}
		if body.SeekForwardSecs != nil {
			forward = *body.SeekForwardSecs
		}
		config.SetSeekSeconds(back, forward)
	}
	if body.DLNAReceiverEnabled != nil {
		config.SetDLNAReceiverEnabled(*body.DLNAReceiverEnabled)
		s.refreshDLNAReceiver(*body.DLNAReceiverEnabled)
	}
	writeJSON(w, http.StatusOK, config.Settings())
}

// resetSettings is the settings danger-zone "reset everything" action: wipes
// every server/account and preference back to defaults.
func (s *Server) resetSettings(w http.ResponseWriter, r *http.Request) {
	settings := config.ResetAll()
	s.refreshDLNAReceiver(true)
	writeJSON(w, http.StatusOK, settings)
}

// ── Server management ────────────────────────────────────────────────────────

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	active := config.ActiveServer()
	activeID := ""
	if active != nil {
		activeID = active.ID
	}
	out := []map[string]any{}
	for _, srv := range config.Servers() {
		m := safeServer(srv)
		m["active"] = srv.ID == activeID
		m["logged_in"] = srv.AccessToken != "" ||
			config.IsFileServerType(srv.Type) ||
			(config.NormalizeServerType(srv.Type) == "iptv" && srv.PlaylistURL != "")
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string   `json:"name"`
		Type        string   `json:"type"`
		Protocol    string   `json:"protocol"`
		Hosts       []string `json:"hosts"`
		Port        int      `json:"port"`
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Token       string   `json:"token"`
		Share       string   `json:"share"`
		Domain      string   `json:"domain"`
		RootPath    string   `json:"root_path"`
		PlaylistURL string   `json:"playlist_url"`
		EPGURL      string   `json:"epg_url"`
	}
	if !decode(r, &in) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}

	candidate := config.Server{
		Name:        in.Name,
		Type:        in.Type,
		Protocol:    in.Protocol,
		Hosts:       in.Hosts,
		Port:        in.Port,
		Share:       in.Share,
		Domain:      in.Domain,
		RootPath:    in.RootPath,
		Username:    in.Username,
		Password:    in.Password,
		PlaylistURL: in.PlaylistURL,
		EPGURL:      in.EPGURL,
	}

	// When credentials are supplied, verify the connection + login BEFORE saving
	// anything: a wrong address or password must not leave a broken server behind
	// Persist only after authentication succeeds. We authenticate against an
	// in-memory candidate and only persist on success. File sources verify with
	// no share/root_path required yet — the folder picker lets the user choose
	// one after this succeeds, instead of typing it up front.
	var token, userID string
	kind := config.NormalizeServerType(candidate.Type)
	shouldVerify := config.IsFileServerType(kind) || kind == "iptv" || in.Token != "" || (in.Username != "" && in.Password != "")
	if shouldVerify {
		var err error
		token, userID, err = provider.Authenticate(&candidate, in.Username, in.Password, in.Token)
		if err != nil {
			writeErr(w, r, err)
			return
		}
	}

	srv := config.AddServer(candidate)
	if token != "" {
		config.SetAuth(srv.ID, in.Username, token, userID)
		srv = config.GetServer(srv.ID)
	}
	if kind == "iptv" {
		// The candidate has no id yet during provider.Authenticate above (the
		// iptv cache is keyed by server id), so the real playlist/EPG fetch
		// happens here, now that AddServer assigned one. A fetch failure rolls
		// the add back rather than leaving a broken source behind.
		if err := iptv.New(srv).Refresh(r.Context()); err != nil {
			config.DeleteServer(srv.ID)
			writeErr(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, safeServer(srv))
}

func (s *Server) editServer(w http.ResponseWriter, r *http.Request) {
	var patch config.ServerPatch
	if !decode(r, &patch) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	updated := config.UpdateServer(r.PathValue("id"), patch)
	if updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	writeJSON(w, http.StatusOK, safeServer(updated))
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) {
	if !config.DeleteServer(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) activateServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SwitchSession  string `json:"switch_session"`
		SwitchSequence int    `json:"switch_sequence"`
	}
	if r.ContentLength != 0 && !decode(r, &body) {
		return
	}
	s.switchMu.Lock()
	if body.SwitchSession != "" && body.SwitchSequence > 0 {
		if body.SwitchSequence <= s.latestSwitch[body.SwitchSession] {
			active := config.ActiveServer()
			activeID := ""
			if active != nil {
				activeID = active.ID
			}
			s.switchMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": false, "active_server_id": activeID})
			return
		}
		s.latestSwitch[body.SwitchSession] = body.SwitchSequence
	}
	s.switchMu.Unlock()
	if !config.SetActiveServer(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": true, "active_server_id": r.PathValue("id")})
}

func (s *Server) switchHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HostIndex int `json:"host_index"`
	}
	if !decode(r, &body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	if !config.SetActiveHost(r.PathValue("id"), body.HostIndex) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "host_index_out_of_range")})
		return
	}
	if playbackServerID, _ := s.player.State()["server_id"].(string); playbackServerID == r.PathValue("id") {
		go s.player.Stop()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) loginServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Token    string `json:"token"`
	}
	if !decode(r, &body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	srv := config.GetServer(r.PathValue("id"))
	if srv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	username := body.Username
	if username == "" {
		username = srv.Username
	}
	password := body.Password
	if password == "" {
		password = srv.Password
	}
	token, userID, err := provider.Authenticate(srv, username, password, body.Token)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !config.IsFileServerType(srv.Type) {
		config.SetAuth(srv.ID, username, token, userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": username})
}

// connectServer (re-)validates a file source's stored connection details —
// reachability + credentials — without persisting anything, so the folder
// picker can be reopened with confidence after editing host/share/creds.
// Unlike loginServer it never calls config.SetAuth (file sources don't have
// a token to store).
func (s *Server) connectServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(r, &body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	srv := config.GetServer(r.PathValue("id"))
	if srv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	candidate := *srv
	if body.Username != "" {
		candidate.Username = body.Username
	}
	if body.Password != "" {
		candidate.Password = body.Password
	}
	if _, _, err := provider.Authenticate(&candidate, candidate.Username, candidate.Password, ""); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── Player ───────────────────────────────────────────────────────────────────

func (s *Server) playerState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.player.State())
}

func (s *Server) playerCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command []any `json:"command"`
	}
	if !decode(r, &body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	writeJSON(w, http.StatusOK, s.player.Command(body.Command))
}

func (s *Server) playerStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.player.Stop())
}

func (s *Server) playerProps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.player.Props())
}

func (s *Server) playItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID      string `json:"server_id"`
		ItemID        string `json:"item_id"`
		SeriesID      string `json:"series_id"`
		SeasonID      string `json:"season_id"`
		Title         string `json:"title"`
		SeriesTitle   string `json:"series_title"`
		EpisodeLabel  string `json:"episode_label"`
		PosterItemID  string `json:"poster_item_id"`
		Path          string `json:"path"`
		ChannelID     string `json:"channel_id"`
		VariantIndex  int    `json:"variant_index"`
		MediaSourceID string `json:"media_source_id"`
	}
	if !decode(r, &req) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	playbackServer := config.GetServer(req.ServerID)
	if req.ServerID == "" {
		playbackServer = config.ActiveServer()
	}
	if playbackServer == nil {
		writeErr(w, r, provider.Errorf(400, "No media source is available. Add one first."))
		return
	}
	if config.IsFileServerType(playbackServer.Type) {
		files, err := provider.FileFromServer(playbackServer.ID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		_, err = files.ResolvePlayURL(req.Path)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		playURL := "http://127.0.0.1:" + strconv.Itoa(s.port) + "/api/files/stream?server_id=" + url.QueryEscape(playbackServer.ID) + "&path=" + url.QueryEscape(req.Path)
		opts := playOpts(playbackServer.ID, req.Path, "", "", req.Title, "", "", "", config.LocalPlaybackPosition(playbackServer.ID, req.Path), "")
		result := s.player.Play(playURL, opts)
		writeJSON(w, http.StatusOK, result)
		return
	}
	if config.NormalizeServerType(playbackServer.Type) == "iptv" {
		iptvClient, err := iptv.FromServer(playbackServer.ID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		ch := iptvClient.ChannelByID(req.ChannelID)
		if ch == nil || len(ch.Variants) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "channel_not_found")})
			return
		}
		variantIndex := req.VariantIndex
		if variantIndex < 0 || variantIndex >= len(ch.Variants) {
			variantIndex = 0
		}
		result := s.player.Play(ch.Variants[variantIndex].URL, player.PlayOptions{ServerID: playbackServer.ID, Title: ch.Name, IsLive: true, ChannelID: ch.ID})
		config.RecordIPTVRecent(playbackServer.ID, ch.ID, 50)
		writeJSON(w, http.StatusOK, result)
		return
	}
	client, err := provider.FromServer(playbackServer.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	choice, err := client.ChoosePlayURL(req.ItemID, req.MediaSourceID)
	if err != nil {
		writeErr(w, r, err)
		return
	}

	url := choice.URL
	mediaSourceID := choice.MediaSourceID

	startSeconds := client.ResumePositionSeconds(req.ItemID)

	opts := playOpts(playbackServer.ID, req.ItemID, req.SeriesID, req.SeasonID,
		req.Title, req.SeriesTitle, req.EpisodeLabel, req.PosterItemID, startSeconds, mediaSourceID)
	result := s.player.Play(url, opts)

	if ok, _ := result["ok"].(bool); ok {
		client.ReportStart(req.ItemID, s.player.PlaySessionID(), mediaSourceID)
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Emby proxy ───────────────────────────────────────────────────────────────

func mediaClient(r *http.Request) (provider.Media, error) {
	if id := r.URL.Query().Get("server_id"); id != "" {
		return provider.FromServer(id)
	}
	return provider.Active()
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
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// filesClient resolves the ?server_id= query param to a Client, falling back
// to the active server — mirrors iptvClient. The folder picker relies on
// server_id to browse a just-created, not-yet-active source.
func filesClient(r *http.Request) (*filesource.Client, error) {
	if id := r.URL.Query().Get("server_id"); id != "" {
		return provider.FileFromServer(id)
	}
	return provider.ActiveFile()
}

func (s *Server) filesList(w http.ResponseWriter, r *http.Request) {
	c, err := filesClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	listing, err := c.ListDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) filesStream(w http.ResponseWriter, r *http.Request) {
	c, err := filesClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := c.Serve(w, r, r.URL.Query().Get("path")); err != nil {
		writeErr(w, r, err)
	}
}

// ── IPTV ─────────────────────────────────────────────────────────────────────

// iptvClient resolves the ?server_id= query param to a Client, falling back
// to the active server so the common case (browsing the currently-selected
// IPTV source) needs no query param at all.
func iptvClient(r *http.Request) (*iptv.Client, error) {
	if id := r.URL.Query().Get("server_id"); id != "" {
		return iptv.FromServer(id)
	}
	return iptv.FromActive()
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

func iptvChannelRow(c *iptv.Client, ch iptv.Channel, favorites map[string]bool) map[string]any {
	row := map[string]any{
		"id":            ch.ID,
		"name":          ch.Name,
		"logo_url":      ch.LogoURL,
		"group_title":   ch.GroupTitle,
		"quality":       ch.Quality,
		"variant_count": len(ch.Variants),
		"is_favorite":   favorites[ch.ID],
		"has_epg":       false,
	}
	if p := c.CurrentProgramme(ch.ID); p != nil {
		row["has_epg"] = true
		row["current_programme"] = map[string]any{"title": p.Title}
	} else {
		row["current_programme"] = nil
	}
	return row
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

	out := []map[string]any{}
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
		out = append(out, iptvChannelRow(c, ch, favorites))
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
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) iptvProgramme(w http.ResponseWriter, r *http.Request) {
	c, err := iptvClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	channelID := r.URL.Query().Get("channel_id")
	count := qInt(r, "count", 4)
	writeJSON(w, http.StatusOK, map[string]any{
		"current":  c.CurrentProgramme(channelID),
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	serverID := iptvBodyServerID(body.ServerID)
	if serverID == "" || body.ChannelID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	serverID := iptvBodyServerID(body.ServerID)
	if serverID == "" || body.ChannelID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	recent := config.RecordIPTVRecent(serverID, body.ChannelID, 50)
	writeJSON(w, http.StatusOK, map[string]any{"recently_watched": recent})
}

// ── System volume ─────────────────────────────────────────────────────────────

func (s *Server) systemVolumeGet(w http.ResponseWriter, r *http.Request) {
	vol, err := sysvolume.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	muted, err := sysvolume.GetMuted()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"volume": vol, "muted": muted})
}

func (s *Server) systemVolumeSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Volume *int  `json:"volume"`
		Muted  *bool `json:"muted"`
	}
	if !decode(r, &req) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid body"})
		return
	}
	result := map[string]any{}
	if req.Volume != nil {
		vol, err := sysvolume.Set(*req.Volume)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
			return
		}
		result["volume"] = vol
	}
	if req.Muted != nil {
		muted, err := sysvolume.SetMuted(*req.Muted)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
			return
		}
		result["muted"] = muted
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Frontend ─────────────────────────────────────────────────────────────────

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.frontendAsset(w, r, "index.html", "text/html; charset=utf-8")
}

func (s *Server) webManifest(w http.ResponseWriter, r *http.Request) {
	s.frontendAsset(w, r, "manifest.webmanifest", "application/manifest+json; charset=utf-8")
}

func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	s.frontendAsset(w, r, "sw.js", "application/javascript; charset=utf-8")
}

func (s *Server) frontendAsset(w http.ResponseWriter, r *http.Request, name, contentType string) {
	data, err := readWeb(s, name)
	if err != nil {
		http.Error(w, i18n.Request(r, "frontend_not_built"), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
