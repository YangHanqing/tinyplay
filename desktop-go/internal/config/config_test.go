package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func useTempData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", dir)
	return dir
}

func TestLegacyServerDefaultsToEmby(t *testing.T) {
	dir := useTempData(t)
	raw := []byte(`{"servers":[{"id":"old","hosts":["nas.local"],"port":8096}]}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if len(cfg.Servers) != 1 || cfg.Servers[0].Type != "emby" {
		t.Fatalf("legacy type = %#v", cfg.Servers)
	}
}

func TestFileSourceAndLanguagePersist(t *testing.T) {
	dir := useTempData(t)
	srv := AddServer(Server{Name: "NAS", Type: "file", FileProtocol: "webdav", Root: "https://nas/dav", Username: "u", Password: "秘密"})
	SetLanguage("zh-CN")
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted Config
	if err = json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Language != "zh-CN" || len(persisted.Servers) != 1 || persisted.Servers[0].ID != srv.ID || persisted.Servers[0].Password != "秘密" {
		t.Fatalf("unexpected persisted config: %#v", persisted)
	}
}
