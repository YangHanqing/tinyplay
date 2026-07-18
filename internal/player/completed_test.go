package player

import "testing"

func TestClearCompletedPlayback(t *testing.T) {
	p := New()
	p.mu.Lock()
	p.ctx = PlayContext{
		ItemID: "e1", SeriesID: "s", SeasonID: "sea", PlaybackCompleted: true,
	}
	rev := p.playbackRevision
	p.mu.Unlock()

	p.ClearCompletedPlayback()
	st := p.State()
	if st["playback_completed"] == true {
		t.Fatal("playback_completed should be cleared")
	}
	if st["item_id"] != "" {
		t.Fatalf("item_id still set: %v", st["item_id"])
	}
	p.mu.Lock()
	if p.playbackRevision <= rev {
		t.Fatal("revision should bump when clearing completed context")
	}
	p.mu.Unlock()

	// No-op when not completed.
	p.ClearCompletedPlayback()
}
