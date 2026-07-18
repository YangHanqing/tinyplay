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
