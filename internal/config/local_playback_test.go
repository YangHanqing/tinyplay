package config

import "testing"

func TestLocalPlaybackPositionTailRule(t *testing.T) {
	useTempData(t)
	const threeHours = 10800.0
	cases := []struct {
		name     string
		path     string
		position float64
		want     float64
	}{
		{"deep into a long file still resumes", "/a.mkv", 10260, 10260}, // 95% — 9 minutes left
		{"within last 15s counts as finished", "/b.mkv", threeHours - 10, 0},
		{"past 99% counts as finished", "/c.mkv", 10700, 0},
		{"under 5s restarts from the top", "/d.mkv", 3, 0},
	}
	for _, tc := range cases {
		RecordLocalPlayback("srv", tc.path, tc.position, threeHours)
		if got := LocalPlaybackPosition("srv", tc.path); got != tc.want {
			t.Errorf("%s: position = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Without a duration the tail cannot be ruled out, so the entry restarts.
// Resuming a short clip at its own tail makes it reach EOF the instant it
// opens, which the host then reads as a completed playback and hands to
// autoplay — the file appears to be skipped.
func TestLocalPlaybackPositionRestartsWhenDurationUnknown(t *testing.T) {
	useTempData(t)
	RecordLocalPlayback("srv", "/clip.mp4", 18, 0)
	if got := LocalPlaybackPosition("srv", "/clip.mp4"); got != 0 {
		t.Fatalf("position = %v, want 0 for an entry with no duration", got)
	}
}

// The position survives an exit that the duration does not: mpv drops the
// duration property at end-of-file, and a clip short enough to finish inside
// one progress interval may never have published it at all. Writing that 0 over
// a duration we already knew would disable the tail rule for this entry.
func TestRecordLocalPlaybackKeepsKnownDuration(t *testing.T) {
	useTempData(t)
	RecordLocalPlayback("srv", "/clip.mp4", 6, 18)
	RecordLocalPlayback("srv", "/clip.mp4", 17, 0)

	var entry LocalPlaybackEntry
	for _, e := range Load().LocalPlaybackHistory {
		if e.Path == "/clip.mp4" {
			entry = e
			break
		}
	}
	if entry.DurationSeconds != 18 {
		t.Fatalf("duration = %v, want the previously known 18", entry.DurationSeconds)
	}
	if entry.PositionSeconds != 17 {
		t.Fatalf("position = %v, want the newly reported 17", entry.PositionSeconds)
	}
	// And with the duration retained, the tail rule can still do its job.
	if got := LocalPlaybackPosition("srv", "/clip.mp4"); got != 0 {
		t.Fatalf("position = %v, want 0 — 17s of an 18s clip is the tail", got)
	}
}
