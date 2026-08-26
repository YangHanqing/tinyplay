//go:build !windows

package player

import "os/exec"

// hideConsole is a Windows-only concern; see probe_windows.go.
func hideConsole(cmd *exec.Cmd) {}
