package player

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedLogFileResetsAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	b := &boundedLogFile{file: f, maxBytes: 8}
	if _, err = b.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Write([]byte("abcd")); err != nil {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcd" {
		t.Fatalf("log=%q", data)
	}
}
