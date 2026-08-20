//go:build darwin

package main

import "tvremote/internal/player"

// wireMonitorHint is a no-op on macOS: the Go core and the native shell are
// separate processes, so there is nothing to query in-process. The shell
// instead pushes its window's monitor via POST /desktop/display/monitor-hint
// (handlers_desktopdisplay.go), which calls player.Player.SetMonitorHint
// directly — no hook needed here.
func wireMonitorHint(p *player.Player) {}
