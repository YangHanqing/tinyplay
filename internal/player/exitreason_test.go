package player

import (
	"errors"
	"strings"
	"testing"
)

func TestHintForExitCodeExplainsLoaderFailures(t *testing.T) {
	// The reported LTSC failure: mpv imports vulkan-1.dll, the machine has no
	// Vulkan runtime, and Windows kills the process with 0xC0000135 before mpv
	// can write a single log line.
	hint := hintForExitCode(statusDLLNotFound)
	if !strings.Contains(hint, "DLL") {
		t.Fatalf("expected a missing-DLL explanation, got %q", hint)
	}
	if hintForExitCode(statusEntryPointNotFound) == "" {
		t.Fatal("expected an explanation for a wrong-version DLL")
	}
	if hintForExitCode(statusInvalidImageFormat) == "" {
		t.Fatal("expected an explanation for a bad image format")
	}
}

func TestHintForExitCodeStaysSilentOnOrdinaryExits(t *testing.T) {
	for _, code := range []uint32{0, 1, 2, 255} {
		if hint := hintForExitCode(code); hint != "" {
			t.Fatalf("exit code %d should carry no hint, got %q", code, hint)
		}
	}
}

func TestExitStatusHintIgnoresNonExitErrors(t *testing.T) {
	if hint := exitStatusHint(nil); hint != "" {
		t.Fatalf("nil error should carry no hint, got %q", hint)
	}
	if hint := exitStatusHint(errors.New("boom")); hint != "" {
		t.Fatalf("plain error should carry no hint, got %q", hint)
	}
}
