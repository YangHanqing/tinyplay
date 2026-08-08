package player

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tvremote/internal/config"
)

// titleSettingsDirty tracks which of the five remembered properties the remote
// actually changed during the current Play. Snapshotting whatever mpv auto-
// selected would permanently override the server's default subtitle track for
// that item, so an untouched property is never persisted.
type titleSettingsDirty struct {
	sid, aid, subDelay, audioDelay, speed bool
}

// pendingTitleRestore is the per-title record waiting for a track-list so sid/
// aid can be resolved after file-loaded. Speed and delays are applied earlier.
type pendingTitleRestore struct {
	entry config.TitleSettingsEntry
	// tracksApplied is set once we have either restored sid/aid or decided
	// there is nothing track-related to restore. Re-entry on later track-list
	// updates is then a no-op.
	tracksApplied bool
}

// titleSettingsEligible reports whether sourceType can own per-title playback
// settings. IPTV and DLNA are excluded: a live channel has no meaningful
// per-title settings, and a DLNA push has no stable id.
func titleSettingsEligible(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "emby", "jellyfin", "plex", "webdav", "smb", "local", "nfs":
		return true
	default:
		// Empty / iptv / iptv-catchup / dlna / unknown.
		return false
	}
}

// sessionSpeedEligible reports whether sourceType inherits the session-scoped
// continuity speed (the keep_playback_speed switch).
//
// Deliberately a separate predicate from titleSettingsEligible, which answers a
// different question: IPTV cannot own per-title settings either, but a channel
// is still something this machine chose to play, so carrying the speed into it
// is coherent. A DLNA cast is not — it is content another device handed over,
// and starting someone's song at the 1.5x left over from the last film reads as
// a malfunction. Observed in the wild: a cast flac launched with --speed=1.5.
func sessionSpeedEligible(sourceType string) bool {
	return strings.ToLower(strings.TrimSpace(sourceType)) != "dlna"
}

// resolvePlaybackSpeed picks the speed for a new title.
// Priority: a stored per-title speed (switch B) wins over the session-scoped
// continuity speed (switch A); with neither, the title starts at 1.0x.
func resolvePlaybackSpeed(keepSession bool, sessionSpeed float64, stored *float64) float64 {
	if stored != nil && *stored > 0 {
		return *stored
	}
	if keepSession && sessionSpeed > 0 {
		return sessionSpeed
	}
	return 1.0
}

// mpvTrack is one entry of mpv's track-list property (subset we care about).
type mpvTrack struct {
	ID       int
	Type     string // "audio", "sub", "video", …
	Lang     string
	Title    string
	Selected bool
}

// parseTrackList converts mpv's track-list property value into a typed slice.
// Unknown / non-array shapes yield an empty list rather than panicking — a
// missing track-list must leave the player's defaults alone.
func parseTrackList(raw any) []mpvTrack {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]mpvTrack, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t := mpvTrack{
			ID:       int(numeric(m["id"])),
			Type:     strings.ToLower(fmt.Sprint(m["type"])),
			Lang:     stringField(m["lang"]),
			Title:    stringField(m["title"]),
			Selected: boolField(m["selected"]),
		}
		out = append(out, t)
	}
	return out
}

func stringField(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

func boolField(v any) bool {
	b, _ := v.(bool)
	return b
}

// resolveTrackID finds an mpv track id for the given kind ("sub"/"audio").
// Language is the primary key and the title only disambiguates, because titles
// are cosmetic and vary between releases while a language tag rarely does.
//
// No match returns ok=false, and the caller then leaves mpv's own selection
// alone. That is the important half of this function: a wrong restore is worse
// than none, since the tracks it would pick from are things like a commentary
// track sitting next to the main audio in the same language.
func resolveTrackID(tracks []mpvTrack, kind string, want config.TrackChoice) (id int, ok bool) {
	if want.Disabled {
		return 0, true // caller maps this to sid/aid "no"
	}
	kind = strings.ToLower(kind)
	var candidates []mpvTrack
	for _, t := range tracks {
		if t.Type != kind {
			continue
		}
		if want.Lang != "" && !strings.EqualFold(t.Lang, want.Lang) {
			continue
		}
		candidates = append(candidates, t)
	}
	switch {
	case len(candidates) == 0:
		return 0, false
	case len(candidates) == 1 && want.Lang != "":
		// Exactly one track in the remembered language: the title cannot make
		// this more specific, so a renamed track still restores correctly.
		return candidates[0].ID, true
	case want.Title == "":
		// Nothing left to disambiguate with. Taking the first is deterministic,
		// and with no stored title there is nothing better to go on.
		return candidates[0].ID, true
	}
	for _, t := range candidates {
		if strings.EqualFold(t.Title, want.Title) {
			return t.ID, true
		}
	}
	// Several candidates and none carries the remembered title: guessing among
	// them is exactly how a commentary track gets selected. Leave the default.
	return 0, false
}

// trackChoiceFromList builds a TrackChoice for the currently selected track of
// the given kind, using lang+title from track-list. A disabled track
// (sid/aid "no"/false/0) yields Disabled=true.
func trackChoiceFromList(tracks []mpvTrack, kind string, current any) *config.TrackChoice {
	if trackDisabled(current) {
		return &config.TrackChoice{Disabled: true}
	}
	id := int(numeric(current))
	if id <= 0 {
		// Fall back to whichever track-list entry is selected.
		for _, t := range tracks {
			if t.Type == kind && t.Selected {
				return &config.TrackChoice{Lang: t.Lang, Title: t.Title}
			}
		}
		return nil
	}
	for _, t := range tracks {
		if t.Type == kind && t.ID == id {
			return &config.TrackChoice{Lang: t.Lang, Title: t.Title}
		}
	}
	return nil
}

func trackDisabled(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return !x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "no" || s == "false" || s == "0"
	case float64:
		return x == 0
	case int:
		return x == 0
	case json.Number:
		f, _ := x.Float64()
		return f == 0
	default:
		s := strings.ToLower(fmt.Sprint(x))
		return s == "no" || s == "false" || s == "0"
	}
}

// dirtyPropsFromCommand returns which of the five remembered properties a
// remote command touches. Only the allow-listed property-mutating verbs can
// mark a field dirty; seek/frame-step and friends never do.
func dirtyPropsFromCommand(cmd []any) (sid, aid, subDelay, audioDelay, speed bool) {
	if len(cmd) < 2 {
		return
	}
	verb, _ := cmd[0].(string)
	if !propertyMutatingVerbs[verb] {
		return
	}
	prop, _ := cmd[1].(string)
	switch prop {
	case "sid":
		sid = true
	case "aid":
		aid = true
	case "sub-delay":
		subDelay = true
	case "audio-delay":
		audioDelay = true
	case "speed":
		speed = true
	}
	return
}

// buildTitleSettingsPatch materialises a dirty-field patch from live props.
// Untouched fields stay nil so RecordTitleSettings leaves prior values alone.
func buildTitleSettingsPatch(dirty titleSettingsDirty, live map[string]any) config.TitleSettingsPatch {
	var p config.TitleSettingsPatch
	tracks := parseTrackList(live["track-list"])
	if dirty.sid {
		if c := trackChoiceFromList(tracks, "sub", live["sid"]); c != nil {
			p.Subtitle = c
		} else if trackDisabled(live["sid"]) {
			p.Subtitle = &config.TrackChoice{Disabled: true}
		}
	}
	if dirty.aid {
		if c := trackChoiceFromList(tracks, "audio", live["aid"]); c != nil {
			p.Audio = c
		} else if trackDisabled(live["aid"]) {
			p.Audio = &config.TrackChoice{Disabled: true}
		}
	}
	if dirty.subDelay {
		v := numeric(live["sub-delay"])
		p.SubDelay = &v
	}
	if dirty.audioDelay {
		v := numeric(live["audio-delay"])
		p.AudioDelay = &v
	}
	if dirty.speed {
		v := numeric(live["speed"])
		if v <= 0 {
			v = 1.0
		}
		p.Speed = &v
	}
	return p
}

func formatMPVFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ── Player wiring ────────────────────────────────────────────────────────────

// snapshotTitleSettings persists dirty per-title fields if the setting is on
// and the current context is eligible. Called from the same points as
// fireStopReport (title switch and mpv exit) so config.json is not rewritten on
// every remote property change.
func (p *Player) snapshotTitleSettings() {
	if !config.Load().RememberTitleSettings {
		return
	}
	p.mu.Lock()
	ctx := p.ctx
	dirty := p.titleDirty
	p.mu.Unlock()
	if !titleSettingsEligible(ctx.SourceType) || ctx.ServerID == "" || ctx.ItemID == "" {
		return
	}
	if !dirty.sid && !dirty.aid && !dirty.subDelay && !dirty.audioDelay && !dirty.speed {
		return
	}
	p.propsMu.Lock()
	live := make(map[string]any, len(p.liveProps))
	for k, v := range p.liveProps {
		live[k] = v
	}
	p.propsMu.Unlock()
	// Session continuity tracks the live speed even when speed was not dirty.
	// Deliberately outside the propsMu section: nothing else in this package
	// holds propsMu while taking p.mu, and keeping it that way is what makes
	// the two locks impossible to deadlock against each other.
	p.observeSessionSpeed(live["speed"])
	delta := buildTitleSettingsPatch(dirty, live)
	config.RecordTitleSettings(ctx.ServerID, ctx.ItemID, delta)
}

// noteDirtyFromCommand marks remembered properties the remote just mutated.
func (p *Player) noteDirtyFromCommand(cmd []any) {
	sid, aid, subDelay, audioDelay, speed := dirtyPropsFromCommand(cmd)
	if !sid && !aid && !subDelay && !audioDelay && !speed {
		return
	}
	verb, _ := cmd[0].(string)
	p.mu.Lock()
	if sid {
		p.titleDirty.sid = true
	}
	if aid {
		p.titleDirty.aid = true
	}
	if subDelay {
		p.titleDirty.subDelay = true
	}
	if audioDelay {
		p.titleDirty.audioDelay = true
	}
	if speed {
		p.titleDirty.speed = true
		// Absolute set_property only: add/multiply carry a delta, not a final
		// value. Property-change events still keep sessionSpeed current.
		if verb == "set_property" && len(cmd) >= 3 {
			if v := numeric(cmd[2]); v > 0 {
				p.sessionSpeed = v
			}
		}
	}
	p.mu.Unlock()
}

// prepareTitleRestore loads any stored per-title record and computes the speed
// that must be applied on this Play (B overrides A). Resets dirty state.
// Returns the speed to apply and any delays from the stored record.
func (p *Player) prepareTitleRestore(opt PlayOptions) (speed float64, subDelay, audioDelay *float64) {
	cfg := config.Load()
	p.mu.Lock()
	// Capture the session speed *before* we reset anything so A can reuse it.
	sessionSpeed := p.sessionSpeed
	if sessionSpeed <= 0 {
		sessionSpeed = 1.0
	}
	p.titleDirty = titleSettingsDirty{}
	p.pendingRestore = nil
	p.mu.Unlock()

	var storedSpeed *float64
	if cfg.RememberTitleSettings && titleSettingsEligible(opt.SourceType) &&
		opt.ServerID != "" && opt.ItemID != "" {
		if entry, ok := config.LookupTitleSettings(opt.ServerID, opt.ItemID); ok {
			storedSpeed = entry.Speed
			subDelay = entry.SubDelay
			audioDelay = entry.AudioDelay
			// Tracks wait for track-list; stash the whole entry.
			p.mu.Lock()
			p.pendingRestore = &pendingTitleRestore{entry: entry}
			p.mu.Unlock()
		}
	}
	if !sessionSpeedEligible(opt.SourceType) {
		// Start at unity and leave the stored session speed alone: a cast must
		// neither inherit this machine's speed nor reset it for whatever the
		// user goes back to afterwards.
		return 1.0, subDelay, audioDelay
	}
	speed = resolvePlaybackSpeed(cfg.KeepPlaybackSpeed, sessionSpeed, storedSpeed)
	p.mu.Lock()
	p.sessionSpeed = speed
	p.mu.Unlock()
	return speed, subDelay, audioDelay
}

// applyEarlyTitleSettings sets speed and the two delays. Silent — no OSD.
// Tracks are restored later via tryRestoreTitleTracks.
func (p *Player) applyEarlyTitleSettings(speed float64, subDelay, audioDelay *float64) {
	// Always set speed explicitly on both the reused-process and fresh-process
	// paths so behaviour does not depend on whether mpv happened to keep the
	// previous speed.
	p.send([]any{"set_property", "speed", speed})
	if subDelay != nil {
		p.send([]any{"set_property", "sub-delay", *subDelay})
	}
	if audioDelay != nil {
		p.send([]any{"set_property", "audio-delay", *audioDelay})
	}
}

// earlyTitleSettingsArgs returns mpv startup flags for speed/delays when a
// fresh process is launched (IPC is not connected yet).
func earlyTitleSettingsArgs(speed float64, subDelay, audioDelay *float64) []string {
	args := []string{"--speed=" + formatMPVFloat(speed)}
	if subDelay != nil {
		args = append(args, "--sub-delay="+formatMPVFloat(*subDelay))
	}
	if audioDelay != nil {
		args = append(args, "--audio-delay="+formatMPVFloat(*audioDelay))
	}
	return args
}

// tryRestoreTitleTracks resolves stored lang+title against the live track-list
// and sets sid/aid. No match leaves mpv's own default alone. Silent — no OSD.
func (p *Player) tryRestoreTitleTracks() {
	p.mu.Lock()
	pending := p.pendingRestore
	if pending == nil || pending.tracksApplied {
		p.mu.Unlock()
		return
	}
	entry := pending.entry
	p.mu.Unlock()

	if entry.Subtitle == nil && entry.Audio == nil {
		p.mu.Lock()
		if p.pendingRestore != nil {
			p.pendingRestore.tracksApplied = true
		}
		p.mu.Unlock()
		return
	}

	p.propsMu.Lock()
	tracks := parseTrackList(p.liveProps["track-list"])
	p.propsMu.Unlock()
	if len(tracks) == 0 {
		// File not fully probed yet; wait for a later track-list update.
		return
	}

	if entry.Subtitle != nil {
		if entry.Subtitle.Disabled {
			p.send([]any{"set_property", "sid", "no"})
		} else if id, ok := resolveTrackID(tracks, "sub", *entry.Subtitle); ok {
			p.send([]any{"set_property", "sid", id})
		}
	}
	if entry.Audio != nil {
		if entry.Audio.Disabled {
			p.send([]any{"set_property", "aid", "no"})
		} else if id, ok := resolveTrackID(tracks, "audio", *entry.Audio); ok {
			p.send([]any{"set_property", "aid", id})
		}
	}
	p.mu.Lock()
	if p.pendingRestore != nil {
		p.pendingRestore.tracksApplied = true
	}
	p.mu.Unlock()
}

// observeSessionSpeed keeps the in-memory session speed current from property
// change events so keep_playback_speed can reapply it on the next title.
func (p *Player) observeSessionSpeed(v any) {
	if sp := numeric(v); sp > 0 {
		p.mu.Lock()
		p.sessionSpeed = sp
		p.mu.Unlock()
	}
}
