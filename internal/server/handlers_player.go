package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/iptv"
	"tvremote/internal/player"
	"tvremote/internal/provider"
)

// playerStateLongPollTimeout bounds GET /api/player/state?after_revision=…
// so clients can hold a single wait without hanging forever if nothing changes.
const playerStateLongPollTimeout = 25 * time.Second

func (s *Server) playerState(w http.ResponseWriter, r *http.Request) {
	if afterRaw := r.URL.Query().Get("after_revision"); afterRaw != "" {
		if after, err := strconv.ParseUint(afterRaw, 10, 64); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), playerStateLongPollTimeout)
			defer cancel()
			// Equal revision → wait for a change (or timeout/cancel).
			// Mismatched revision → WaitPlaybackRevision returns immediately.
			s.player.WaitPlaybackRevision(ctx, after)
		}
		// Malformed after_revision is ignored: fall through to an immediate
		// state snapshot so ordinary clients stay compatible.
	}
	writeJSON(w, http.StatusOK, s.mergeAutoplayState(s.player.State()))
}

func (s *Server) playerCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command []any `json:"command"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.player.Command(body.Command))
}

func (s *Server) playerStop(w http.ResponseWriter, r *http.Request) {
	s.CancelAutoplay(false)
	s.invalidatePlay()
	writeJSON(w, http.StatusOK, s.player.Stop())
}

func (s *Server) playerNext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.PlayAutoplayNow())
}

func (s *Server) playerNextCancel(w http.ResponseWriter, r *http.Request) {
	s.CancelAutoplay(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func supersededPlay(w http.ResponseWriter) {
	// Keep the historical 200 response shape for /player/play. The frontend can
	// quietly discard this result instead of showing the earlier user a false
	// playback error after a later request has legitimately won.
	writeJSON(w, http.StatusOK, map[string]any{"ok": false, "superseded": true})
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
		CatchupStart  string `json:"catchup_start"`
		CatchupStop   string `json:"catchup_stop"`
		MediaSourceID string `json:"media_source_id"`
	}
	if !decode(r, &req) {
		invalidBody(w, r)
		return
	}
	// A new explicit play supersedes any pending host autoplay transition.
	s.CancelAutoplay(false)
	generation := s.beginPlay()
	playbackServer := config.GetServer(req.ServerID)
	if req.ServerID == "" {
		playbackServer = config.ActiveServer()
	}
	if playbackServer == nil {
		s.player.RecordExternalFailure("unknown", "source_selection")
		writeErr(w, r, provider.Errorf(400, "No media source is available. Add one first."))
		return
	}
	if config.IsFileServerType(playbackServer.Type) {
		files, err := provider.FileFromServer(playbackServer.ID)
		if err != nil {
			s.player.RecordExternalFailure(config.NormalizeServerType(playbackServer.Type), "source_connect")
			writeErr(w, r, err)
			return
		}
		_, err = files.ResolvePlayURL(req.Path)
		if err != nil {
			s.player.RecordExternalFailure(config.NormalizeServerType(playbackServer.Type), "media_open")
			writeErr(w, r, err)
			return
		}
		playURL := "http://127.0.0.1:" + strconv.Itoa(s.port) + "/api/files/stream?server_id=" + url.QueryEscape(playbackServer.ID) + "&path=" + url.QueryEscape(req.Path)
		opts := playOpts(playbackServer.ID, req.Path, "", "", req.Title, "", "", "", config.LocalPlaybackPosition(playbackServer.ID, req.Path), "")
		opts.SourceType = config.NormalizeServerType(playbackServer.Type)
		if !s.isCurrentPlay(generation) {
			supersededPlay(w)
			return
		}
		result := s.player.Play(playURL, opts)
		writeJSON(w, http.StatusOK, result)
		return
	}
	if config.NormalizeServerType(playbackServer.Type) == "iptv" {
		iptvClient, err := iptv.FromServer(playbackServer.ID)
		if err != nil {
			s.player.RecordExternalFailure("iptv", "source_connect")
			writeErr(w, r, err)
			return
		}
		ch := iptvClient.ChannelByID(req.ChannelID)
		if ch == nil || len(ch.Variants) == 0 {
			s.player.RecordExternalFailure("iptv", "server_negotiation")
			writeJSON(w, http.StatusNotFound, map[string]string{"detail": i18n.Request(r, "channel_not_found")})
			return
		}
		variantIndex := req.VariantIndex
		if variantIndex < 0 || variantIndex >= len(ch.Variants) {
			variantIndex = 0
		}
		if !s.isCurrentPlay(generation) {
			supersededPlay(w)
			return
		}
		stream := ch.Variants[variantIndex]
		isLive := true
		sourceType := "iptv"
		title := ch.Name
		if req.CatchupStart != "" || req.CatchupStop != "" {
			start, startErr := time.Parse(time.RFC3339, req.CatchupStart)
			stop, stopErr := time.Parse(time.RFC3339, req.CatchupStop)
			if startErr != nil || stopErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Invalid catch-up programme time"})
				return
			}
			stream, err = iptvClient.CatchupStream(ch.ID, variantIndex, start, stop)
			if err != nil {
				s.player.RecordExternalFailure("iptv", "catchup_url")
				writeErr(w, r, err)
				return
			}
			isLive = false
			sourceType = "iptv-catchup"
			if req.Title != "" {
				title = req.Title
			}
		}
		result := s.player.Play(stream.URL, player.PlayOptions{
			ServerID: playbackServer.ID, Title: title, IsLive: isLive,
			ChannelID: ch.ID, VariantIndex: variantIndex, SourceType: sourceType,
			HTTPHeaders: stream.HTTPHeaders,
		})
		// Echo the normalized index (rather than the untrusted request value) so
		// the requesting remote highlights the stream the player actually loaded.
		result["variant_index"] = variantIndex
		result["is_catchup"] = !isLive
		writeJSON(w, http.StatusOK, result)
		return
	}
	client, err := provider.FromServer(playbackServer.ID)
	if err != nil {
		s.player.RecordExternalFailure(config.NormalizeServerType(playbackServer.Type), "source_connect")
		writeErr(w, r, err)
		return
	}
	choice, err := client.ChoosePlayURL(req.ItemID, req.MediaSourceID)
	if err != nil {
		s.player.RecordExternalFailure(config.NormalizeServerType(playbackServer.Type), "server_negotiation")
		writeErr(w, r, err)
		return
	}

	url := choice.URL
	mediaSourceID := choice.MediaSourceID

	startSeconds := client.ResumePositionSeconds(req.ItemID)

	opts := playOpts(playbackServer.ID, req.ItemID, req.SeriesID, req.SeasonID,
		req.Title, req.SeriesTitle, req.EpisodeLabel, req.PosterItemID, startSeconds, mediaSourceID)
	opts.SourceType = config.NormalizeServerType(playbackServer.Type)
	if !s.isCurrentPlay(generation) {
		supersededPlay(w)
		return
	}
	result := s.player.Play(url, opts)

	if ok, _ := result["ok"].(bool); ok && s.isCurrentPlay(generation) {
		client.ReportStart(req.ItemID, s.player.PlaySessionID(), mediaSourceID)
	}
	writeJSON(w, http.StatusOK, result)
}
