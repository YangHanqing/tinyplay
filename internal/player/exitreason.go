package player

import (
	"errors"
	"os/exec"
)

// Windows kills a process inside the loader — before any of the program's own
// code runs — when a static import cannot be resolved. mpv therefore writes
// nothing at all: no window, no mpv log, and the only evidence left behind is
// an opaque status code in the exit status. That is exactly what a bundled
// player missing a DLL looks like from here, so translate the codes we can
// recognise into something a bug report can act on.
const (
	statusDLLNotFound        = 0xC0000135
	statusEntryPointNotFound = 0xC0000139
	statusInvalidImageFormat = 0xC000007B
)

// exitStatusHint returns a short explanation for a process exit status that
// means the executable never started, or "" when the exit says nothing special.
func exitStatusHint(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil {
		return ""
	}
	return hintForExitCode(uint32(exitErr.ProcessState.ExitCode()))
}

func hintForExitCode(code uint32) string {
	switch code {
	case statusDLLNotFound:
		return "a DLL the player depends on is missing on this machine, so it " +
			"was killed before it could run (its log will be empty)"
	case statusEntryPointNotFound:
		return "a DLL the player depends on is present but the wrong version, " +
			"so it was killed before it could run (its log will be empty)"
	case statusInvalidImageFormat:
		return "the player or one of its DLLs is not a valid 64-bit Windows " +
			"binary, so it was killed before it could run"
	}
	return ""
}
