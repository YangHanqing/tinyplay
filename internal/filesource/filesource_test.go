package filesource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"tvremote/internal/config"
)

func TestWebDAVBrowseAndResolve(t *testing.T) {
	var authOK bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		authOK = ok && u == "alice" && p == "秘密"
		if r.Method == "GET" {
			if r.Header.Get("Range") != "bytes=2-4" {
				t.Errorf("range=%q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("cde"))
			return
		}
		if r.Method != "PROPFIND" {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response><d:response><d:href>%s/Movies/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response><d:response><d:href>%s/demo%%20movie.mkv</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>1234</d:getcontentlength></d:prop></d:propstat></d:response></d:multistatus>`, r.URL.Path[:len(r.URL.Path)-1], r.URL.Path[:len(r.URL.Path)-1], r.URL.Path[:len(r.URL.Path)-1])
	}))
	defer ts.Close()
	tsURL, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(tsURL.Port())
	c := New(&config.Server{Name: "DAV", Type: "webdav", Protocol: tsURL.Scheme, Hosts: []string{tsURL.Hostname()}, Port: port, RootPath: "dav", Username: "alice", Password: "秘密"})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if !authOK || len(listing.Entries) != 2 || !listing.Entries[0].IsDir || !listing.Entries[1].IsVideo {
		t.Fatalf("listing=%#v auth=%v", listing, authOK)
	}
	u, err := c.ResolvePlayURL("demo movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if u == "" || u[:4] != "http" {
		t.Fatalf("url=%q", u)
	}
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	if err := c.Serve(rec, req, "demo movie.mkv"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "cde" {
		t.Fatalf("stream %d %q", rec.Code, rec.Body.String())
	}
}

func TestLocalBrowseFiltersNonVideo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: dir})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 || !listing.Entries[0].IsDir || listing.Entries[1].Name != "movie.mkv" {
		t.Fatalf("%#v", listing.Entries)
	}
}

func TestLocalBrowseWithoutRootPathListsOSRoot(t *testing.T) {
	// No RootPath configured yet (fresh "local" source, still picking a
	// folder): browsing must list the real OS root instead of erroring or
	// silently resolving to the process's CWD.
	c := New(&config.Server{Name: "Local", Type: "local"})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) == 0 {
		t.Fatalf("expected the OS root to have at least one browsable entry, got none")
	}
}

func TestLocalRelativeRootPathResolvesFromOSRootNotCWD(t *testing.T) {
	// The folder picker persists a bare picked segment (e.g. "Applications"),
	// not an absolute path — it must resolve against the true OS root, not
	// the Go process's own working directory (regression: it silently
	// resolved against os.Getwd() and returned "Folder not found").
	osRoot, err := filepath.Abs(string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(osRoot)
	if err != nil {
		t.Skip("OS root not listable in this sandbox")
	}
	var name string
	for _, e := range entries {
		if e.IsDir() {
			name = e.Name()
			break
		}
	}
	if name == "" {
		t.Skip("no top-level directory found under the OS root")
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: name})
	if _, err := c.ListDir(""); err != nil {
		t.Fatalf("relative root_path %q should resolve from the OS root: %v", name, err)
	}
}

func TestSMBServeRequiresShare(t *testing.T) {
	c := New(&config.Server{Type: "smb", Hosts: []string{"nas"}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	err := c.Serve(rec, req, "movie.mkv")
	if err == nil {
		t.Fatal("expected missing share error")
	}
}

func TestSMBListWithoutShareRequiresHost(t *testing.T) {
	// Share == "" and no path segments means "enumerate the host's shares" —
	// exercised for real in TestWebDAVBrowseAndResolve-style integration only
	// (needs a live SMB server); here we just confirm the missing-host guard
	// fires before any network dial is attempted.
	c := New(&config.Server{Type: "smb"})
	if _, err := c.ListDir(""); err == nil {
		t.Fatal("expected missing host error")
	}
}
