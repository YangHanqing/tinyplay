package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	const password = "s3cret!"
	srv := AddServer(Server{Name: "NAS", Type: "webdav", Hosts: []string{"nas"}, Protocol: "https", RootPath: "dav", Username: "u", Password: password})
	SetLanguage("zh-CN")
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted Config
	if err = json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Language != "zh-CN" || len(persisted.Servers) != 1 || persisted.Servers[0].ID != srv.ID || persisted.Servers[0].Password != password {
		t.Fatalf("unexpected persisted config: %#v", persisted)
	}
}

func TestHostsCappedAtThree(t *testing.T) {
	useTempData(t)
	srv := AddServer(Server{Name: "Home", Hosts: []string{"a", " ", "b", "c", "d"}})
	if len(srv.Hosts) != 3 || srv.Hosts[0] != "a" || srv.Hosts[1] != "b" || srv.Hosts[2] != "c" {
		t.Fatalf("hosts = %#v", srv.Hosts)
	}
	patch := ServerPatch{Hosts: &[]string{"x", "y", "z", "w"}}
	updated := UpdateServer(srv.ID, patch)
	if len(updated.Hosts) != 3 || updated.Hosts[2] != "z" {
		t.Fatalf("patched hosts = %#v", updated.Hosts)
	}
}

func TestBuildServerURLHonorsCompleteURLAndBasePath(t *testing.T) {
	tests := []struct {
		name   string
		server Server
		want   string
	}{
		{
			name:   "complete HTTPS URL overrides HTTP form fields",
			server: Server{Type: "jellyfin", Protocol: "http", Port: 8096, Hosts: []string{"https://demo.jellyfin.org/stable"}},
			want:   "https://demo.jellyfin.org:443/stable",
		},
		{
			name:   "explicit URL port wins",
			server: Server{Type: "jellyfin", Protocol: "http", Port: 8096, Hosts: []string{"https://demo.jellyfin.org:9443/stable"}},
			want:   "https://demo.jellyfin.org:9443/stable",
		},
		{
			name:   "normalized host uses configured base path",
			server: Server{Type: "jellyfin", Protocol: "https", Port: 443, Hosts: []string{"demo.jellyfin.org"}, BasePath: "stable"},
			want:   "https://demo.jellyfin.org:443/stable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildServerURL(&tt.server); got != tt.want {
				t.Fatalf("BuildServerURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlankServerNamesIncludeSourceTypeAndAddress(t *testing.T) {
	useTempData(t)
	cases := []struct {
		server Server
		want   string
	}{
		{Server{Type: "emby", Hosts: []string{"192.168.1.10"}}, "Emby - 192.168.1.10"},
		{Server{Type: "smb", Hosts: []string{"nas.local"}}, "SMB - nas.local"},
		{Server{Type: "iptv", PlaylistURL: "https://tv.example:8443/list.m3u"}, "IPTV - tv.example"},
	}
	for _, tc := range cases {
		srv := AddServer(tc.server)
		if srv.Name != tc.want {
			t.Fatalf("default name = %q, want %q", srv.Name, tc.want)
		}
	}

	blank := "   "
	updated := UpdateServer(AddServer(Server{Name: "Custom", Type: "plex", Hosts: []string{"plex.local"}}).ID, ServerPatch{Name: &blank})
	if updated.Name != "Plex - plex.local" {
		t.Fatalf("updated blank name = %q", updated.Name)
	}
}

func TestLegacyFileSourceMigrates(t *testing.T) {
	dir := useTempData(t)
	raw := []byte(`{"servers":[
		{"id":"dav1","type":"file","file_protocol":"webdav","root":"https://dav.example.com:8443/media/movies","username":"alice","password":"pw"},
		{"id":"smb1","type":"file","file_protocol":"smb","root":"smb://nas.local/Share/Sub/Path"},
		{"id":"loc1","type":"file","file_protocol":"local","root":"/Volumes/NAS/Movies"}
	]}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if len(cfg.Servers) != 3 {
		t.Fatalf("servers = %#v", cfg.Servers)
	}
	dav, smb, loc := cfg.Servers[0], cfg.Servers[1], cfg.Servers[2]

	if dav.Type != "webdav" || len(dav.Hosts) != 1 || dav.Hosts[0] != "dav.example.com" || dav.Port != 8443 || dav.RootPath != "media/movies" || dav.Root != "" || dav.FileProtocol != "" {
		t.Fatalf("webdav migration = %#v", dav)
	}
	if smb.Type != "smb" || len(smb.Hosts) != 1 || smb.Hosts[0] != "nas.local" || smb.Port != 445 || smb.Share != "Share" || smb.RootPath != "Sub/Path" || smb.Root != "" {
		t.Fatalf("smb migration = %#v", smb)
	}
	if loc.Type != "local" || loc.RootPath != "/Volumes/NAS/Movies" || loc.Root != "" {
		t.Fatalf("local migration = %#v", loc)
	}

	// The migration must actually stick on disk, not just in the in-memory
	// Load() result, otherwise every request would re-migrate from scratch.
	raw2, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw2), `"root"`) || strings.Contains(string(raw2), `"file_protocol"`) {
		t.Fatalf("legacy fields should not round-trip back to disk: %s", raw2)
	}
}
