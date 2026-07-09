package player

import "testing"

func TestStateIncludesPlaybackServerID(t *testing.T) {
	p := &Player{ctx: PlayContext{ServerID: "source-a", ItemID: "movie-a"}}
	state := p.State()
	if got := state["server_id"]; got != "source-a" {
		t.Fatalf("server_id = %#v, want source-a", got)
	}
}
