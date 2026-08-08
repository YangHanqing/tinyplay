package player

import (
	"testing"
	"time"
)

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

func TestCompletedAutoplayContextIncludesFileSources(t *testing.T) {
	for _, sourceType := range []string{"local", "nfs", "smb", "webdav"} {
		t.Run(sourceType, func(t *testing.T) {
			finished := PlayContext{
				ServerID: "files", ItemID: "Season/02.mkv", Title: "02",
				SourceType: sourceType,
			}
			completed, ok := completedAutoplayContext(finished, true, time.Hour)
			if !ok {
				t.Fatalf("natural EOF for %s did not reach autoplay hand-off", sourceType)
			}
			if !completed.PlaybackCompleted || completed.ItemID != finished.ItemID || completed.SourceType != sourceType {
				t.Fatalf("completed context = %+v", completed)
			}
		})
	}
}

func TestCompletedAutoplayContextKeepsExistingExclusions(t *testing.T) {
	cases := []struct {
		name       string
		ctx        PlayContext
		naturalEOF bool
	}{
		{name: "live", ctx: PlayContext{ItemID: "stream", SourceType: "nfs", IsLive: true}, naturalEOF: true},
		{name: "error exit", ctx: PlayContext{ItemID: "episode", SourceType: "nfs"}, naturalEOF: false},
		{name: "empty item", ctx: PlayContext{SourceType: "nfs"}, naturalEOF: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := completedAutoplayContext(tc.ctx, tc.naturalEOF, time.Hour); ok {
				t.Fatal("unexpected autoplay EOF hand-off")
			}
		})
	}
}

// A file opened at (or just before) its own tail hits EOF immediately. That is
// a degenerate open, not a finished title, and must not chain into autoplay —
// otherwise one poisoned resume point walks the whole folder in seconds.
func TestCompletedAutoplayContextRejectsEOFWithoutProgress(t *testing.T) {
	ctx := PlayContext{ServerID: "files", ItemID: "Season/02.mkv", SourceType: "smb"}
	for _, progressed := range []time.Duration{0, 500 * time.Millisecond, -time.Second} {
		if _, ok := completedAutoplayContext(ctx, true, progressed); ok {
			t.Fatalf("EOF after %s of progress was treated as a completed playback", progressed)
		}
	}
	if _, ok := completedAutoplayContext(ctx, true, minAutoplayProgress); !ok {
		t.Fatal("a real viewing must still reach the autoplay hand-off")
	}
}

func TestCompletedAutoplayContextDefersSourcePolicyToServer(t *testing.T) {
	completed, ok := completedAutoplayContext(PlayContext{
		ItemID: "movie", SourceType: "emby",
	}, true, time.Hour)
	if !ok || !completed.PlaybackCompleted {
		t.Fatal("player must hand completed items to the server eligibility policy")
	}
}
