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
