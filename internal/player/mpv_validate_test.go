package player

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeScript drops an executable shell script at dir/name that prints
// output to stdout when run. Skips on Windows: ValidateMPV shells out to the
// executable directly (no interpreter lookup), and a `.sh` script is not
// runnable there without one.
func writeFakeScript(t *testing.T, dir, name, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake mpv script requires a POSIX shell")
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho '" + output + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateMPVAcceptsMPVLikeOutput(t *testing.T) {
	path := writeFakeScript(t, t.TempDir(), "fake-mpv", "mpv 0.36.0 Copyright (C) 2000-2024")
	if err := ValidateMPV(path); err != nil {
		t.Fatalf("expected valid mpv, got error: %v", err)
	}
}

func TestValidateMPVRejectsNonMPVOutput(t *testing.T) {
	path := writeFakeScript(t, t.TempDir(), "not-mpv", "GNU bash, version 5.2")
	if err := ValidateMPV(path); err == nil {
		t.Fatal("expected an error for a non-mpv executable")
	}
}

func TestValidateMPVRejectsMissingPath(t *testing.T) {
	if err := ValidateMPV(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing path")
	}
}

func TestValidateMPVRejectsEmptyPath(t *testing.T) {
	if err := ValidateMPV("   "); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestValidateMPVRejectsDirectory(t *testing.T) {
	if err := ValidateMPV(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory")
	}
}
