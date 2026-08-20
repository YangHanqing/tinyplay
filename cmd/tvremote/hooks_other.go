//go:build !darwin && !windows

package main

import "tvremote/internal/player"

// wireMonitorHint is a no-op on the dev-only Linux target; mpv keeps its
// default display placement there.
func wireMonitorHint(p *player.Player) {}
