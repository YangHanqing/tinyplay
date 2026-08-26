//go:build windows

package player

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps the mpv --version probe from flashing a console window.
//
// DetectMPV probes a candidate at core startup and again whenever a new
// executable turns up, and a console-subsystem mpv.exe (some builds are)
// would otherwise pop a black window on the desktop each time. The user is
// never asking for a window here — the probe exists only to learn whether the
// binary starts at all — so this is not a behaviour change for anyone whose
// player already works. Playback itself does not go through here.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
