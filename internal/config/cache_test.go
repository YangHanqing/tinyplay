package config

import "testing"

func TestNormalizeMpvCacheSecsUsesPresets(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 300}, {300, 300}, {900, 900}, {1800, 1800}, {3600, 3600},
		{600, 300}, {1200, 900}, {7200, 3600},
	}
	for _, tc := range cases {
		if got := NormalizeMpvCacheSecs(tc.in); got != tc.want {
			t.Errorf("NormalizeMpvCacheSecs(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
