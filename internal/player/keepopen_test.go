package player

import (
	"strings"
	"testing"
)

// Keep-open must be forced off so user mpv.conf cannot leave the process
// parked at EOF (which would prevent host-owned next-episode autoplay).
func TestKeepOpenFlagPresentInSpawnArgs(t *testing.T) {
	args := initialMPVArgs("http://example/video", "/tmp/test")
	found := false
	for _, a := range args {
		if a == "--keep-open=no" || strings.HasPrefix(a, "--keep-open=no") {
			found = true
		}
	}
	if !found {
		t.Fatal("spawn args must force --keep-open=no")
	}
}
