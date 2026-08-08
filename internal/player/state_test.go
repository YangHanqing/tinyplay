package player

import "testing"

func TestStateIncludesPlaybackIdentity(t *testing.T) {
	p := &Player{ctx: PlayContext{ServerID: "source-a", ItemID: "movie-a", ChannelID: "channel-a", VariantIndex: 2}, playbackRevision: 7}
	state := p.State()
	if got := state["server_id"]; got != "source-a" {
		t.Fatalf("server_id = %#v, want source-a", got)
	}
	if got := state["variant_index"]; got != 2 {
		t.Fatalf("variant_index = %#v, want 2", got)
	}
	if got := state["playback_revision"]; got != uint64(7) {
		t.Fatalf("playback_revision = %#v, want 7", got)
	}
}

// The remote hides aspect / subtitle / audio-track controls for a song, so the
// verdict has to reach it. It is made once, from the extension table (or the
// cast's DIDL class) that also decides mpv's artwork handling — the frontend
// must never re-derive it from a filename or a track list.
func TestStateReportsAudioOnly(t *testing.T) {
	if got := (&Player{ctx: PlayContext{ItemID: "Album/01.flac", AudioOnly: true}}).State()["audio_only"]; got != true {
		t.Fatalf("audio_only = %#v, want true", got)
	}
	if got := (&Player{ctx: PlayContext{ItemID: "Show/S01E01.mkv"}}).State()["audio_only"]; got != false {
		t.Fatalf("audio_only = %#v, want false", got)
	}
}
