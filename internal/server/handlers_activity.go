package server

import "net/http"

// The activity surface names the single UI the device is currently handing to
// the phone. It is DERIVED on every read from the two authoritative states that
// already exist — the website broker and the mpv player — rather than stored as
// a third copy that could drift out of sync.
//
// Logical ownership is mutually exclusive by construction: opening Website
// first asks mpv to stop (website.Broker.stopMPV, wired in New), and starting
// mpv first requests Website teardown (Player.BeforePlay → RequestWebsiteClose).
// Native processes can overlap briefly while those asynchronous teardowns are
// applied, but the newest requested owner is unambiguous. The broker's own
// closeGen/openGen and the player's playbackRevision already reject stale work,
// so this read model does not need a third generation machine.
//
// Boundary: the website side deliberately exposes only the allowlisted site_id,
// never the real document URL, matching /api/website/state.
const (
	activitySurfaceWebsite  = "website"
	activitySurfacePlayback = "playback"
	activitySurfaceIdle     = "idle"
	activityPhaseOpening    = "opening"
	activityPhaseActive     = "active"
	activityPhaseStarting   = "starting"
	activityPhaseTransition = "transitioning"
	activityPhaseIdle       = "idle"
)

// activityState answers "which surface owns the device right now" so a phone
// that just (re)loaded can restore the correct workspace instead of always
// snapping back to the media library.
func (s *Server) activityState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.activitySnapshot())
}

func (s *Server) activitySnapshot() map[string]any {
	// A pending Website open owns the device immediately after it has requested
	// mpv stop; ReportedOpen then distinguishes opening from active.
	if websiteBroker != nil {
		snap := websiteBroker.Snapshot()
		if snap.DesiredOpen || snap.ReportedOpen {
			phase := activityPhaseOpening
			if snap.ReportedOpen {
				phase = activityPhaseActive
			}
			return map[string]any{
				"surface":     activitySurfaceWebsite,
				"phase":       phase,
				"engine":      "webview",
				"source_type": "website",
				"website": map[string]any{
					"kind":          "website",
					"site_id":       snap.CurrentSiteID,
					"desired_open":  snap.DesiredOpen,
					"reported_open": snap.ReportedOpen,
					"last_status":   snap.LastStatus,
				},
			}
		}
	}

	// Otherwise the mpv player owns the device when it is running or still
	// advertising a context the phone would restore.
	state := s.mergeAutoplayState(s.player.State())
	if activityPlaybackPresent(state) {
		phase := activityPlaybackPhase(state)
		sourceType, _ := state["source_type"].(string)
		return map[string]any{
			"surface":     activitySurfacePlayback,
			"phase":       phase,
			"engine":      "mpv",
			"source_type": sourceType,
			"playback": map[string]any{
				"kind":              "playback",
				"phase":             phase,
				"running":           state["running"],
				"source_type":       sourceType,
				"playback_revision": state["playback_revision"],
			},
		}
	}

	return map[string]any{"surface": activitySurfaceIdle, "phase": activityPhaseIdle}
}

// activityPlaybackPresent mirrors the phone's own restore logic: a running mpv,
// or a non-empty context that restorePlayerContext would repaint, counts as
// playback. A finished episode kept alive only for history (completed, not
// running, no host autoplay pending) is treated as idle here — exactly as the
// phone clears its now-playing chrome in that case — so the surface never lies
// that something is still playing.
func activityPlaybackPresent(state map[string]any) bool {
	if running, _ := state["running"].(bool); running {
		return true
	}
	if completed, _ := state["playback_completed"].(bool); completed {
		if !activityAutoplayCoordinating(state) {
			return false
		}
		return true
	}
	for _, key := range []string{"item_id", "channel_id", "title"} {
		if v, _ := state[key].(string); v != "" {
			return true
		}
	}
	return false
}

func activityAutoplayCoordinating(state map[string]any) bool {
	status, _ := state["autoplay_status"].(string)
	return status == autoplayStatusFindingNext || status == autoplayStatusNextAvailable
}

func activityPlaybackPhase(state map[string]any) string {
	if activityAutoplayCoordinating(state) {
		return activityPhaseTransition
	}
	if running, _ := state["running"].(bool); running {
		return activityPhaseActive
	}
	return activityPhaseStarting
}
