package server

import (
	"net/http"
	"runtime"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/iptv"
	"tvremote/internal/player"
	"tvremote/internal/provider"
)

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
	writeJSON(w, http.StatusOK, s.runtimeSettings())
}

// runtimeSettings appends live, non-persisted capabilities to the configured
// settings. In particular, the DLNA toggle alone cannot promise playback if
// its mpv runtime is unavailable.
func (s *Server) runtimeSettings() map[string]any {
	settings := config.Settings()
	settings["platform"] = runtime.GOOS
	mpv := player.DetectMPV()
	settings["mpv_available"] = mpv.Available
	settings["mpv_source"] = mpv.Source
	settings["dlna_receiver_status"] = s.dlnaReceiverStatus()
	return settings
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MpvCacheSecs        *int    `json:"mpv_cache_secs"`
		Language            *string `json:"language"`
		SeekBackwardSecs    *int    `json:"seek_backward_secs"`
		SeekForwardSecs     *int    `json:"seek_forward_secs"`
		DLNAReceiverEnabled *bool   `json:"dlna_receiver_enabled"`
		AutoplayNextEpisode *bool   `json:"autoplay_next_episode"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
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
	if body.AutoplayNextEpisode != nil {
		config.SetAutoplayNextEpisode(*body.AutoplayNextEpisode)
		if !*body.AutoplayNextEpisode {
			// Turning the setting off mid-countdown must not still autoplay.
			s.CancelAutoplay(true)
		}
	}
	writeJSON(w, http.StatusOK, s.runtimeSettings())
}

// resetSettings is the settings danger-zone "reset everything" action: wipes
// every server/account and preference back to defaults.
func (s *Server) resetSettings(w http.ResponseWriter, r *http.Request) {
	s.CancelAutoplay(true)
	settings := config.ResetAll()
	resetWebsiteState()
	iptv.ClearCaches()
	// Drive the receiver from the actual post-reset value rather than a
	// hardcoded true, so the two can't drift if the reset default changes.
	enabled, _ := settings["dlna_receiver_enabled"].(bool)
	s.refreshDLNAReceiver(enabled)
	writeJSON(w, http.StatusOK, s.runtimeSettings())
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
		invalidBody(w, r)
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
			iptv.RemoveCache(srv.ID)
			writeErr(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, safeServer(srv))
}

func (s *Server) editServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		config.ServerPatch
		Validate bool   `json:"validate"`
		Token    string `json:"token"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	current := config.GetServer(r.PathValue("id"))
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	candidate := config.ApplyServerPatch(*current, body.ServerPatch)
	var token, userID string
	if body.Validate {
		var err error
		kind := config.NormalizeServerType(candidate.Type)
		switch {
		case kind == "iptv":
			err = iptv.New(&candidate).Refresh(r.Context())
		case config.IsFileServerType(kind):
			_, _, err = provider.Authenticate(&candidate, candidate.Username, candidate.Password, "")
		case body.Token != "" || body.ServerPatch.Password != nil:
			token, userID, err = provider.Authenticate(&candidate, candidate.Username, candidate.Password, body.Token)
		default:
			// Address-only edits retain the existing access token. Probe the
			// candidate before committing it so a typo cannot strand a source.
			media, mediaErr := provider.FromServerConfig(&candidate)
			if mediaErr != nil {
				err = mediaErr
			} else {
				_, err = media.Libraries()
			}
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
	}
	updated := config.UpdateServer(r.PathValue("id"), body.ServerPatch)
	if updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	if token != "" {
		config.SetAuth(updated.ID, candidate.Username, token, userID)
		updated = config.GetServer(updated.ID)
	}
	writeJSON(w, http.StatusOK, safeServer(updated))
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !config.DeleteServer(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "server_not_found")})
		return
	}
	iptv.RemoveCache(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) activateServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SwitchSession  string `json:"switch_session"`
		SwitchSequence int    `json:"switch_sequence"`
	}
	if r.ContentLength != 0 && !decode(r, &body) {
		invalidBody(w, r)
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
		invalidBody(w, r)
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
		invalidBody(w, r)
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
		invalidBody(w, r)
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
