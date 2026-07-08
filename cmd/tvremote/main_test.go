package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateLogKeepsBoundedHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvremote.log")
	for generation := 0; generation < 5; generation++ {
		if err := os.WriteFile(path, []byte("oversized"), 0o644); err != nil {
			t.Fatal(err)
		}
		rotateLog(path, 1, 3)
	}
	for _, suffix := range []string{".1", ".2", ".3"} {
		if _, err := os.Stat(path + suffix); err != nil {
			t.Fatalf("missing backup %s: %v", suffix, err)
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth backup: %v", err)
	}
}
