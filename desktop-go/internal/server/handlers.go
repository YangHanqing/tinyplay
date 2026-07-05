package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strconv"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
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
		"file_protocol":   s.FileProtocol,
		"root":            s.Root,
	}
}

// ── App settings ────────────────────────────────────────────────────────────

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, config.Settings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MpvCacheSecs *int    `json:"mpv_cache_secs"`
		Language     *string `json:"language"`
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
	writeJSON(w, http.StatusOK, config.Settings())
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
		m["logged_in"] = srv.AccessToken != "" || (config.NormalizeServerType(srv.Type) == "file" && srv.Root != "")
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Protocol     string   `json:"protocol"`
		Hosts        []string `json:"hosts"`
		Port         int      `json:"port"`
		Username     string   `json:"username"`
		Password     string   `json:"password"`
		Token        string   `json:"token"`
		FileProtocol string   `json:"file_protocol"`
		Root         string   `json:"root"`
	}
	if !decode(r, &in) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}

	candidate := config.Server{
		Name:         in.Name,
		Type:         in.Type,
		Protocol:     in.Protocol,
		Hosts:        in.Hosts,
		Port:         in.Port,
		FileProtocol: in.FileProtocol,
		Root:         in.Root,
		Username:     in.Username,
		Password:     in.Password,
	}

	// When credentials are supplied, verify the connection + login BEFORE saving
	// anything: a wrong address or password must not leave a broken server behind
	// Persist only after authentication succeeds. We authenticate against an
	// in-memory candidate and only persist on success.
	var token, userID string
	kind := config.NormalizeServerType(candidate.Type)
	shouldVerify := kind == "file" || in.Token != "" || (in.Username != "" && in.Password != "")
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
	if !config.SetActiveServer(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	if config.NormalizeServerType(srv.Type) != "file" {
		config.SetAuth(srv.ID, username, token, userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": username})
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
		ItemID       string `json:"item_id"`
		SeriesID     string `json:"series_id"`
		SeasonID     string `json:"season_id"`
		Title        string `json:"title"`
		SeriesTitle  string `json:"series_title"`
		EpisodeLabel string `json:"episode_label"`
		PosterItemID string `json:"poster_item_id"`
		Path         string `json:"path"`
	}
	if !decode(r, &req) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": i18n.Request(r, "invalid_body")})
		return
	}
	active := config.ActiveServer()
	if active == nil {
		writeErr(w, r, provider.Errorf(400, "No media source is available. Add one first."))
		return
	}
	if config.NormalizeServerType(active.Type) == "file" {
		files, err := provider.ActiveFile()
		if err != nil {
			writeErr(w, r, err)
			return
		}
		_, err = files.ResolvePlayURL(req.Path)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		playURL := "http://127.0.0.1:" + strconv.Itoa(s.port) + "/api/files/stream?path=" + url.QueryEscape(req.Path)
		opts := playOpts("", "", "", req.Title, "", "", "", 0, "")
		result := s.player.Play(playURL, opts)
		writeJSON(w, http.StatusOK, result)
		return
	}
	client, err := provider.Active()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	choice, err := client.ChoosePlayURL(req.ItemID)
	if err != nil {
		writeErr(w, r, err)
		return
	}

	url := choice.URL
	mediaSourceID := choice.MediaSourceID

	// If the user has selected AVPlayer in the engine picker, try to get an
	// Emby remux URL that AVPlayer can play. Fall back to the direct URL if
	// Emby can't produce one (AVPlayer still attempts to play it).
	useNative := s.preferNative && runtime.GOOS == "darwin"
	if useNative && client.Kind() != "plex" {
		if nativeURL, nativeMSID, ok := client.NativePlayURL(req.ItemID); ok {
			url, mediaSourceID = nativeURL, nativeMSID
		}
		log.Printf("Using Apple Player, MediaSourceId=%s", mediaSourceID)
	}

	startSeconds := client.ResumePositionSeconds(req.ItemID)

	opts := playOpts(req.ItemID, req.SeriesID, req.SeasonID,
		req.Title, req.SeriesTitle, req.EpisodeLabel, req.PosterItemID, startSeconds, mediaSourceID)
	opts.UseNative = useNative
	result := s.player.Play(url, opts)

	if ok, _ := result["ok"].(bool); ok {
		client.ReportStart(req.ItemID, s.player.PlaySessionID(), mediaSourceID)
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Emby proxy ───────────────────────────────────────────────────────────────

func (s *Server) embyLibraries(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.Libraries()
	writeRaw(w, r, body, err)
}

func (s *Server) embyResume(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.Resume(12)
	writeRaw(w, r, body, err)
}

func (s *Server) embyItems(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
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
	writeRaw(w, r, body, err)
}

func (s *Server) embyItemDetail(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	body, err := c.ItemDetailRaw(r.PathValue("item_id"))
	writeRaw(w, r, body, err)
}

func (s *Server) embyEpisodes(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
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

func (s *Server) embyImage(w http.ResponseWriter, r *http.Request) {
	c, err := provider.Active()
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

func (s *Server) filesList(w http.ResponseWriter, r *http.Request) {
	c, err := provider.ActiveFile()
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
	c, err := provider.ActiveFile()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := c.Serve(w, r, r.URL.Query().Get("path")); err != nil {
		writeErr(w, r, err)
	}
}

// ── Player engine picker ──────────────────────────────────────────────────────

func (s *Server) engineGet(w http.ResponseWriter, r *http.Request) {
	engine := "mpv"
	if s.preferNative {
		engine = "avplayer"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"engine":   engine,
		"platform": runtime.GOOS,
	})
}

func (s *Server) engineSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine string `json:"engine"`
	}
	if !decode(r, &req) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid body"})
		return
	}
	switch req.Engine {
	case "avplayer":
		s.preferNative = true
	default:
		s.preferNative = false
	}
	engine := "mpv"
	if s.preferNative {
		engine = "avplayer"
	}
	log.Printf("Player engine switched to: %s", engine)
	writeJSON(w, http.StatusOK, map[string]string{"engine": engine})
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
