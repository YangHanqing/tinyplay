//go:build windows

package autostart

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// useTestValueName points the package at a throwaway Run value so the test
// never adds, rewrites, or deletes the real "TinyPlay" startup entry on the
// developer's or CI runner's account.
func useTestValueName(t *testing.T) {
	t.Helper()
	original := runValueName
	runValueName = "TinyPlayAutostartTest"
	t.Cleanup(func() {
		if k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE); err == nil {
			_ = k.DeleteValue(runValueName)
			k.Close()
		}
		runValueName = original
	})
}

func TestEnableDisableRoundTrip(t *testing.T) {
	useTestValueName(t)

	on, err := enabled()
	if err != nil {
		t.Fatalf("enabled: %v", err)
	}
	if on {
		t.Fatal("a value we have not written yet reports as enabled")
	}

	if err := setEnabled(true); err != nil {
		t.Fatalf("setEnabled(true): %v", err)
	}
	if on, err = enabled(); err != nil || !on {
		t.Fatalf("after enabling: on=%v err=%v", on, err)
	}

	// The command must survive a path containing spaces, which is why it is
	// quoted, and must carry the login-launch flag the shell looks for.
	value, err := readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, autostartFlag) {
		t.Fatalf("unexpected startup command: %q", value)
	}

	if err := setEnabled(false); err != nil {
		t.Fatalf("setEnabled(false): %v", err)
	}
	if on, err = enabled(); err != nil || on {
		t.Fatalf("after disabling: on=%v err=%v", on, err)
	}
	// Disabling twice is what a user gets by clicking the checked item when
	// something already removed the entry; it must not surface an error.
	if err := setEnabled(false); err != nil {
		t.Fatalf("setEnabled(false) twice: %v", err)
	}
}

// Sync repairs a stale path (an in-place upgrade into a different directory)
// and stays a no-op while autostart is off.
func TestSyncRepairsStalePathAndSkipsWhenOff(t *testing.T) {
	useTestValueName(t)

	if err := syncPath(); err != nil {
		t.Fatalf("syncPath while off: %v", err)
	}
	if value, err := readValue(); err != nil || value != "" {
		t.Fatalf("syncPath created an entry while off: %q (%v)", value, err)
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if err := k.SetStringValue(runValueName, `"C:\Old\TinyPlay.exe" `+autostartFlag); err != nil {
		k.Close()
		t.Fatalf("SetStringValue: %v", err)
	}
	k.Close()

	if err := syncPath(); err != nil {
		t.Fatalf("syncPath: %v", err)
	}
	want, err := startupCommand()
	if err != nil {
		t.Fatalf("startupCommand: %v", err)
	}
	if value, _ := readValue(); value != want {
		t.Fatalf("stale path not repaired: got %q want %q", value, want)
	}
}
