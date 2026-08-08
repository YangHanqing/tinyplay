package player

import (
	"testing"

	"tvremote/internal/config"
)

func TestResolveTrackIDByLangThenTitle(t *testing.T) {
	tracks := []mpvTrack{
		{ID: 1, Type: "audio", Lang: "eng", Title: "English"},
		{ID: 2, Type: "audio", Lang: "jpn", Title: "Japanese"},
		{ID: 3, Type: "audio", Lang: "jpn", Title: "Commentary"},
		{ID: 1, Type: "sub", Lang: "eng", Title: "English"},
		{ID: 2, Type: "sub", Lang: "chi", Title: "简体"},
	}

	id, ok := resolveTrackID(tracks, "audio", config.TrackChoice{Lang: "jpn", Title: "Japanese"})
	if !ok || id != 2 {
		t.Fatalf("lang+title match: id=%d ok=%v, want 2/true", id, ok)
	}
	// Two tracks share the language and neither carries the remembered title:
	// guessing here is how the commentary track gets selected, so no match.
	if id, ok := resolveTrackID(tracks, "audio", config.TrackChoice{Lang: "jpn", Title: "Director"}); ok {
		t.Fatalf("ambiguous lang with a mismatched title must not resolve, got id=%d", id)
	}
	// But when the language is unambiguous the title is only cosmetic, so a
	// release that renamed the track still restores it.
	if id, ok := resolveTrackID(tracks, "sub", config.TrackChoice{Lang: "chi", Title: "Simplified Chinese"}); !ok || id != 2 {
		t.Fatalf("sole track in the remembered language: id=%d ok=%v, want 2/true", id, ok)
	}
	// No match at all leaves the player's default alone.
	if id, ok := resolveTrackID(tracks, "audio", config.TrackChoice{Lang: "kor", Title: "Korean"}); ok {
		t.Fatalf("no-match must return ok=false, got id=%d", id)
	}
	// Disabled maps to a synthetic success so the caller can set "no".
	if id, ok := resolveTrackID(tracks, "sub", config.TrackChoice{Disabled: true}); !ok || id != 0 {
		t.Fatalf("disabled: id=%d ok=%v, want 0/true", id, ok)
	}
	// Case-insensitive lang + title.
	id, ok = resolveTrackID(tracks, "sub", config.TrackChoice{Lang: "CHI", Title: "简体"})
	if !ok || id != 2 {
		t.Fatalf("case-insensitive lang: id=%d ok=%v, want 2/true", id, ok)
	}
}

func TestResolvePlaybackSpeedPriority(t *testing.T) {
	stored := 1.5
	// B (stored) overrides A (session).
	if got := resolvePlaybackSpeed(true, 2.0, &stored); got != 1.5 {
		t.Fatalf("B should win: got %v", got)
	}
	// No stored record: A applies.
	if got := resolvePlaybackSpeed(true, 2.0, nil); got != 2.0 {
		t.Fatalf("A on with session speed: got %v", got)
	}
	// A off: always 1.0 even if session still holds 2.0.
	if got := resolvePlaybackSpeed(false, 2.0, nil); got != 1.0 {
		t.Fatalf("A off: got %v, want 1.0", got)
	}
	// A off but B has a stored speed: B still wins (independent switches).
	if got := resolvePlaybackSpeed(false, 2.0, &stored); got != 1.5 {
		t.Fatalf("B with A off: got %v", got)
	}
	// Neither: 1.0.
	if got := resolvePlaybackSpeed(false, 0, nil); got != 1.0 {
		t.Fatalf("neither: got %v", got)
	}
}

func TestTitleSettingsEligibleExcludesIPTVAndDLNA(t *testing.T) {
	for _, kind := range []string{"emby", "jellyfin", "plex", "webdav", "smb", "local", "nfs"} {
		if !titleSettingsEligible(kind) {
			t.Errorf("%s should be eligible", kind)
		}
	}
	for _, kind := range []string{"iptv", "iptv-catchup", "dlna", "", "unknown"} {
		if titleSettingsEligible(kind) {
			t.Errorf("%s must be excluded", kind)
		}
	}
}

// Session speed and per-title settings answer different questions, so the two
// predicates are deliberately not the same set: IPTV cannot own per-title
// settings, but a channel is still something this machine chose to play. Only a
// cast is excluded from inheriting the speed.
func TestSessionSpeedEligibleExcludesOnlyDLNA(t *testing.T) {
	for _, kind := range []string{"emby", "jellyfin", "plex", "webdav", "smb", "local", "nfs", "iptv", "iptv-catchup", "", "unknown"} {
		if !sessionSpeedEligible(kind) {
			t.Errorf("%s should inherit the session speed", kind)
		}
	}
	for _, kind := range []string{"dlna", "DLNA", " dlna "} {
		if sessionSpeedEligible(kind) {
			t.Errorf("%q must not inherit the session speed", kind)
		}
	}
}

// A cast starts at 1.0x and leaves the session speed alone: the user's 1.5x
// belongs to what they were watching, not to a song another device pushed, and
// must still be there when they go back to it.
func TestDLNACastNeitherInheritsNorResetsSessionSpeed(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	p := &Player{sessionSpeed: 1.5}
	speed, _, _ := p.prepareTitleRestore(PlayOptions{SourceType: "dlna", ItemID: "http://sender/song.flac"})
	if speed != 1.0 {
		t.Fatalf("cast speed = %v, want 1.0", speed)
	}
	p.mu.Lock()
	kept := p.sessionSpeed
	p.mu.Unlock()
	if kept != 1.5 {
		t.Fatalf("session speed = %v after a cast, want it left at 1.5", kept)
	}
}

func TestDirtyPropsFromCommand(t *testing.T) {
	sid, aid, sub, audio, speed := dirtyPropsFromCommand([]any{"set_property", "sid", 2})
	if !sid || aid || sub || audio || speed {
		t.Fatalf("sid-only: %v %v %v %v %v", sid, aid, sub, audio, speed)
	}
	sid, aid, sub, audio, speed = dirtyPropsFromCommand([]any{"add", "sub-delay", 0.1})
	if sid || aid || !sub || audio || speed {
		t.Fatalf("sub-delay add: %v %v %v %v %v", sid, aid, sub, audio, speed)
	}
	// Seek must never mark a remembered field dirty.
	sid, aid, sub, audio, speed = dirtyPropsFromCommand([]any{"seek", 10})
	if sid || aid || sub || audio || speed {
		t.Fatalf("seek dirtied fields: %v %v %v %v %v", sid, aid, sub, audio, speed)
	}
}

func TestBuildTitleSettingsPatchOnlyDirtyFields(t *testing.T) {
	live := map[string]any{
		"sid":         float64(2),
		"aid":         float64(1),
		"sub-delay":   0.5,
		"audio-delay": -0.1,
		"speed":       1.75,
		"track-list": []any{
			map[string]any{"id": float64(1), "type": "audio", "lang": "eng", "title": "English", "selected": true},
			map[string]any{"id": float64(2), "type": "sub", "lang": "jpn", "title": "Japanese", "selected": true},
		},
	}
	// Only speed was touched by the remote — an untouched subtitle must not
	// be snapshotted, or the server's default track is frozen forever.
	patch := buildTitleSettingsPatch(titleSettingsDirty{speed: true}, live)
	if patch.Speed == nil || *patch.Speed != 1.75 {
		t.Fatalf("speed patch = %v", patch.Speed)
	}
	if patch.Subtitle != nil || patch.Audio != nil || patch.SubDelay != nil || patch.AudioDelay != nil {
		t.Fatalf("untouched fields must stay nil: %+v", patch)
	}

	patch = buildTitleSettingsPatch(titleSettingsDirty{sid: true, aid: true}, live)
	if patch.Subtitle == nil || patch.Subtitle.Lang != "jpn" || patch.Subtitle.Title != "Japanese" {
		t.Fatalf("subtitle choice = %+v", patch.Subtitle)
	}
	if patch.Audio == nil || patch.Audio.Lang != "eng" {
		t.Fatalf("audio choice = %+v", patch.Audio)
	}
	if patch.Speed != nil {
		t.Fatalf("speed must not be dirty: %v", patch.Speed)
	}
}

func TestEarlyTitleSettingsArgs(t *testing.T) {
	sub := 0.25
	args := earlyTitleSettingsArgs(1.5, &sub, nil)
	want := []string{"--speed=1.5", "--sub-delay=0.25"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
	// A off → 1.0 still appears so a fresh process never inherits mpv.conf speed.
	args = earlyTitleSettingsArgs(1.0, nil, nil)
	if len(args) != 1 || args[0] != "--speed=1" {
		t.Fatalf("default speed args = %v", args)
	}
}

func TestNoteDirtyFromCommandMarksOnlyTouched(t *testing.T) {
	p := &Player{sessionSpeed: 1.0}
	p.noteDirtyFromCommand([]any{"set_property", "speed", 2.0})
	if !p.titleDirty.speed || p.titleDirty.sid {
		t.Fatalf("dirty = %+v", p.titleDirty)
	}
	if p.sessionSpeed != 2.0 {
		t.Fatalf("sessionSpeed = %v, want 2.0", p.sessionSpeed)
	}
	// add carries a delta; dirty yes, but sessionSpeed stays for property-change.
	p.noteDirtyFromCommand([]any{"add", "sub-delay", 0.1})
	if !p.titleDirty.subDelay {
		t.Fatal("sub-delay should be dirty after add")
	}
}
