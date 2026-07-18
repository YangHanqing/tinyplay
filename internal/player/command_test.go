package player

import "testing"

func TestAllowedRemoteCommand(t *testing.T) {
	allowed := [][]any{
		{"cycle", "pause"},
		{"set_property", "pause", true},
		{"set_property", "speed", 1.5},
		{"set_property", "sid", "no"},
		{"set_property", "aid", 2},
		{"set_property", "video-aspect-override", "no"},
		{"set_property", "panscan", 1},
		{"set_property", "loop-file", "yes"},
		{"set_property", "volume", 80}, // DLNA RenderingControl
		{"set_property", "mute", true},
		{"seek", -5},
		{"seek", 120, "absolute"},
		{"add", "sub-delay", 0.1},
	}
	for _, cmd := range allowed {
		if !allowedRemoteCommand(cmd) {
			t.Errorf("expected %v to be allowed", cmd)
		}
	}

	denied := [][]any{
		{},                             // empty
		{"run", "/bin/sh", "-c", "id"}, // arbitrary program execution
		{"set_property", "input-ipc-server", "/tmp/x"}, // dangerous property
		{"set_property", "sub-file-paths", "/etc"},     // reaches the filesystem
		{"loadfile", "http://evil/x"},                  // playback is started via /api/player/play, not here
		{"quit"},                                       // stop is a dedicated endpoint
		{"screenshot"},                                 // not a control verb
		{"cycle"},                                      // property-mutating verb missing its property
		{"set_property"},                               // missing property + value
		{123, "pause"},                                 // non-string verb
	}
	for _, cmd := range denied {
		if allowedRemoteCommand(cmd) {
			t.Errorf("expected %v to be denied", cmd)
		}
	}
}
