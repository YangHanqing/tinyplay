package server

import "testing"

func TestActionLabelsAreAlwaysEnglish(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/api/player/state", "Get player state"},
		{"POST", "/api/player/play", "Play"},
		{"GET", "/api/emby/libraries", "Get libraries"},
		{"POST", "/api/servers", "Add Media Source"},
		{"PUT", "/api/servers/source-id", "Edit Media Source"},
		{"GET", "/api/settings", "Settings"},
	}
	for _, tc := range cases {
		if got := actionLabel(tc.method, tc.path); got != tc.want {
			t.Errorf("actionLabel(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}
