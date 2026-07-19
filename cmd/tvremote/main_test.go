package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveNonEnglishLog(t *testing.T) {
	// archiveNonEnglishLog triggers on unicode.Han; keep a single Han marker
	// (escaped) so the test source stays free of product Chinese copy.
	const hanMarker = "\u6c49"
	path := filepath.Join(t.TempDir(), "tvremote.log")
	if err := os.WriteFile(path, []byte("TinyPlay ready\nlegacy-marker: "+hanMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveNonEnglishLog(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("active non-English log should be archived, stat error = %v", err)
	}
	matches, err := filepath.Glob(path + ".pre-english-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one archived log, matches = %v, err = %v", matches, err)
	}
	contents, err := os.ReadFile(matches[0])
	if err != nil || !strings.Contains(string(contents), hanMarker) {
		t.Fatalf("archived log was not preserved: %q, err = %v", contents, err)
	}
}

func TestArchiveNonEnglishLogLeavesEnglishLogInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tvremote.log")
	if err := os.WriteFile(path, []byte("TinyPlay ready\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveNonEnglishLog(path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("English log should remain active: %v", err)
	}
}

func TestRotatingLogFileRotatesWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tvremote.log")
	f, err := newRotatingLogFile(path, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close rotating log: %v", err)
		}
	})
	if _, err := f.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("789")); err != nil {
		t.Fatal(err)
	}

	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "789" || string(backup) != "123456" {
		t.Fatalf("active=%q backup=%q", active, backup)
	}
}
