// Package filesource browses and resolves local, SMB, WebDAV, and NFS-mount
// sources.
package filesource

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
	"tvremote/internal/config"
)

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string   { return e.Msg }
func (e *APIError) StatusCode() int { return e.Status }
func errf(status int, format string, args ...any) *APIError {
	return &APIError{status, fmt.Sprintf(format, args...)}
}

type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	IsVideo bool   `json:"is_video"`
	// IsAudio is independent of IsVideo: a playable audio file, never a
	// video. Clients that only understand is_video keep working; the phone
	// UI uses is_audio for the music glyph.
	IsAudio bool  `json:"is_audio"`
	Size    int64 `json:"size"`
}
type Crumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type Listing struct {
	Path       string  `json:"path"`
	Parent     *string `json:"parent"`
	Breadcrumb []Crumb `json:"breadcrumb"`
	Entries    []Entry `json:"entries"`
}
type Client struct{ server *config.Server }

func New(server *config.Server) *Client { return &Client{server: server} }
func FromActive() (*Client, error) {
	s := config.ActiveServer()
	if s == nil || !config.IsFileServerType(s.Type) {
		return nil, errf(400, "No file source is available")
	}
	return New(s), nil
}

// FromServer returns a Client for a specific server id, for endpoints that
// browse a server that isn't (yet) the active one — e.g. the add-source
// folder picker, which browses a just-created, not-yet-activated server.
// Mirrors iptv.FromServer.
func FromServer(id string) (*Client, error) {
	s := config.GetServer(id)
	if s == nil || !config.IsFileServerType(s.Type) {
		return nil, errf(400, "No such file source")
	}
	return New(s), nil
}

func (c *Client) kind() string { return config.NormalizeServerType(c.server.Type) }

var videoExtensions = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".ts": true, ".m2ts": true, ".mts": true, ".mov": true, ".webm": true, ".flv": true, ".wmv": true, ".mpg": true, ".mpeg": true, ".iso": true, ".rmvb": true, ".rm": true, ".vob": true, ".ogv": true, ".divx": true, ".asf": true, ".3gp": true, ".f4v": true, ".mpv": true, ".dav": true}

// audioExtensions are formats mpv can play as audio. Kept disjoint from
// videoExtensions so IsVideo stays a strict video contract (published to
// clients) and AudioOnly presentation is never set for a real video track.
var audioExtensions = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
	".opus": true, ".wav": true, ".wma": true, ".alac": true, ".aiff": true,
	".aif": true, ".ape": true, ".wv": true, ".dsf": true, ".dff": true,
	".mka": true, ".mp2": true, ".ac3": true, ".dts": true, ".tta": true,
	".spx": true, ".oga": true, ".m4b": true,
}

func isVideo(name string) bool { return videoExtensions[strings.ToLower(filepath.Ext(name))] }

// IsAudio reports whether name's extension is a playable audio file. Exported
// because the player wiring decides mpv's AudioOnly presentation from it, and
// that decision must be made from this one table — never guessed.
func IsAudio(name string) bool { return audioExtensions[strings.ToLower(filepath.Ext(name))] }

// isListedFile keeps directories plus playable video/audio; everything else
// (subs, nfo, images, …) stays hidden from the folder browser.
func isListedFile(name string) bool { return isVideo(name) || IsAudio(name) }
func segments(path string) ([]string, error) {
	out := []string{}
	for _, raw := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		// Do not TrimSpace segment names: network volumes (esp. Linux NAS
		// mounts on Windows) can have leading/trailing spaces that Explorer
		// preserves. Stripping them makes shallow-root dig-down miss folders
		// that work when the same path is stored whole as RootPath.
		if raw == "" || raw == "." {
			continue
		}
		if raw == ".." {
			return nil, errf(400, "Path traversal is not allowed")
		}
		out = append(out, raw)
	}
	return out, nil
}
func (c *Client) listing(segs []string, entries []Entry) Listing {
	visible := entries[:0]
	for _, e := range entries {
		if !strings.HasPrefix(e.Name, ".") {
			visible = append(visible, e)
		}
	}
	entries = visible
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	crumbs := []Crumb{{Name: c.server.Name, Path: ""}}
	for i, s := range segs {
		crumbs = append(crumbs, Crumb{Name: s, Path: strings.Join(segs[:i+1], "/")})
	}
	var parent *string
	if len(segs) > 0 {
		p := strings.Join(segs[:len(segs)-1], "/")
		parent = &p
	}
	return Listing{strings.Join(segs, "/"), parent, crumbs, entries}
}
func entry(segs []string, name string, dir bool, size int64) Entry {
	return Entry{
		Name:    name,
		Path:    strings.Join(append(append([]string{}, segs...), name), "/"),
		IsDir:   dir,
		IsVideo: !dir && isVideo(name),
		IsAudio: !dir && IsAudio(name),
		Size:    size,
	}
}

// Verify validates reachability/credentials without requiring a chosen
// share or root path — a freshly-created source (just a host, or for SMB
// just a host with no share yet) must verify successfully so the folder
// picker can open and let the user pick the path instead of typing it.
func (c *Client) Verify() error {
	_, err := c.ListDir("")
	return err
}

func (c *Client) ListDir(path string) (Listing, error) {
	segs, e := segments(path)
	if e != nil {
		return Listing{}, e
	}
	switch c.kind() {
	case "local", "nfs":
		return c.listLocal(segs)
	case "webdav":
		return c.listWebDAV(segs)
	case "smb":
		return c.listSMB(segs)
	default:
		return Listing{}, errf(400, "Unsupported file source type: %s", c.kind())
	}
}

// host returns the configured Hosts[0], erroring if none is set — every
// network kind (webdav/smb) needs at least a host before it can do
// anything, including the initial Verify().
func (c *Client) host() (string, error) {
	if len(c.server.Hosts) == 0 || strings.TrimSpace(c.server.Hosts[0]) == "" {
		return "", errf(400, "A host address is required")
	}
	return strings.TrimSpace(c.server.Hosts[0]), nil
}

// ── local / nfs ──────────────────────────────────────────────────────────────
//
// NFS has no distinct client implementation here: once the OS has the share
// mounted, it's indistinguishable from a local folder, so nfs reuses the
// exact same browsing code as local. The two stay separate Server "types"
// purely for their label/hint text in the picker.

// localBase resolves the effective directory to browse from and the
// remaining path segments still to descend. If RootPath is unset, it falls
// back to osRootBase (platform-specific: roots_unix.go lists "/" directly,
// roots_windows.go lists drive letters first).
func (c *Client) localBase(segs []string) (base string, rest []string, drivePicker bool) {
	root := osNormalizeConfiguredRoot(strings.TrimSpace(c.server.RootPath))
	if root == "" {
		return osRootBase(segs)
	}
	if !filepath.IsAbs(root) {
		// Picked via the folder browser starting at the OS root (e.g.
		// "Applications" on Unix — on Windows the first segment is always a
		// drive letter, e.g. "C:/Users/bob", which filepath.IsAbs already
		// treats as absolute) — resolve it against that same root instead of
		// the Go process's own working directory.
		root = filepath.Join(string(filepath.Separator), root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, false
	}
	// Windows: Abs("Z:") is drive-relative and can yield just "Z:". Force a
	// real root directory so Join/Rel stay on the volume root.
	if vol := filepath.VolumeName(abs); vol != "" && abs == vol {
		abs = vol + string(filepath.Separator)
	}
	return abs, segs, false
}

func (c *Client) localPath(segs []string) (string, error) {
	base, rest, drivePicker := c.localBase(segs)
	if drivePicker {
		return "", errf(400, "Choose a drive first")
	}
	// A relative base cannot be related to an absolute target, so filepath.Rel
	// below would fail and be misread as a traversal attempt. Reject it here
	// as what it actually is: a root this source cannot browse.
	if base == "" || !filepath.IsAbs(base) {
		return "", errf(400, "Invalid root path")
	}
	// Prefer Clean over Abs when the base is already absolute. Abs walks
	// GetFullPathName, which on Windows is still sensitive to the classic
	// MAX_PATH surface for deep mapped-drive trees; Clean only normalizes
	// separators / "." / ".." and does not re-resolve through that API.
	full := filepath.Join(append([]string{base}, rest...)...)
	if filepath.IsAbs(full) {
		full = filepath.Clean(full)
	} else {
		var e error
		full, e = filepath.Abs(full)
		if e != nil {
			return "", e
		}
	}
	// Two different failures, deliberately not collapsed into one message: a
	// Rel error means the two paths are not relatable at all (different
	// volumes, a malformed base), while a ".." result is a genuine escape
	// attempt. Reporting the first as traversal sends anyone debugging it
	// looking at the requested path instead of at the root.
	rel, e := filepath.Rel(base, full)
	if e != nil {
		return "", errf(400, "Path is outside this source's root")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errf(400, "Path traversal is not allowed")
	}
	return full, nil
}

func (c *Client) listLocal(segs []string) (Listing, error) {
	if _, _, drivePicker := c.localBase(segs); drivePicker {
		return c.listing(segs, driveEntries()), nil
	}
	full, e := c.localPath(segs)
	if e != nil {
		return Listing{}, e
	}
	fsPath := osFilesystemPath(full)
	items, e := os.ReadDir(fsPath)
	// Some virtual / network drive roots reject the extended-length prefix on
	// open while still listing with the classic path. Retry once.
	if e != nil && fsPath != full {
		items, e = os.ReadDir(full)
	}
	if e != nil {
		return Listing{}, localListError(full, e)
	}
	out := []Entry{}
	for _, item := range items {
		dir := item.IsDir()
		if !dir && !isListedFile(item.Name()) {
			continue
		}
		size := int64(0)
		if info, x := item.Info(); x == nil {
			size = info.Size()
		}
		out = append(out, entry(segs, item.Name(), dir, size))
	}
	return c.listing(segs, out), nil
}

// ── webdav ───────────────────────────────────────────────────────────────────

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}
type davResponse struct {
	Href      string        `xml:"href"`
	Propstats []davPropstat `xml:"propstat"`
}
type davPropstat struct {
	Prop davProp `xml:"prop"`
}
type davProp struct {
	ResourceType davResourceType `xml:"resourcetype"`
	Length       int64           `xml:"getcontentlength"`
}
type davResourceType struct {
	Collection *struct{} `xml:"collection"`
}

func (c *Client) remoteURL(segs []string) (string, error) {
	host, e := c.host()
	if e != nil {
		return "", e
	}
	proto := c.server.Protocol
	if proto == "" {
		proto = "http"
	}
	port := c.server.Port
	if port == 0 {
		if proto == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	u := &url.URL{Scheme: proto, Host: net.JoinHostPort(host, fmtPort(port))}
	parts := segments0(c.server.RootPath)
	for _, s := range segs {
		parts = append(parts, s)
	}
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = url.PathEscape(p)
	}
	u.Path = "/" + strings.Join(escaped, "/")
	return u.String(), nil
}

// segments0 is segments() without the path-traversal guard, for splitting a
// trusted, already-stored RootPath rather than untrusted request input.
func segments0(path string) []string {
	out := []string{}
	for _, raw := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		if raw == "" || raw == "." {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func fmtPort(p int) string {
	if p <= 0 {
		return "0"
	}
	return fmt.Sprintf("%d", p)
}

func (c *Client) listWebDAV(segs []string) (Listing, error) {
	u, e := c.remoteURL(segs)
	if e != nil {
		return Listing{}, e
	}
	u = strings.TrimRight(u, "/") + "/"
	req, e := http.NewRequest("PROPFIND", u, nil)
	if e != nil {
		return Listing{}, e
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	if c.server.Username != "" {
		req.SetBasicAuth(c.server.Username, c.server.Password)
	}
	client := http.Client{Timeout: 20 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return Listing{}, errf(502, "Could not list WebDAV folder: %v", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return Listing{}, errf(401, "Authentication failed. Please sign in again.")
	}
	if resp.StatusCode >= 400 {
		return Listing{}, errf(502, "WebDAV returned HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var doc davMultistatus
	if xml.Unmarshal(body, &doc) != nil {
		return Listing{}, errf(502, "WebDAV returned invalid XML")
	}
	self, _ := url.Parse(u)
	selfPath := strings.TrimRight(self.Path, "/")
	out := []Entry{}
	for _, r := range doc.Responses {
		href, _ := url.Parse(r.Href)
		p := strings.TrimRight(href.Path, "/")
		if p == "" || p == selfPath {
			continue
		}
		name, e := url.PathUnescape(filepath.Base(p))
		if e != nil || name == "" {
			continue
		}
		dir := false
		size := int64(0)
		for _, ps := range r.Propstats {
			if ps.Prop.ResourceType.Collection != nil {
				dir = true
			}
			if ps.Prop.Length > size {
				size = ps.Prop.Length
			}
		}
		if !dir && !isListedFile(name) {
			continue
		}
		out = append(out, entry(segs, name, dir, size))
	}
	return c.listing(segs, out), nil
}

// ── smb ──────────────────────────────────────────────────────────────────────

func (c *Client) smbHostPort() (string, error) {
	host, e := c.host()
	if e != nil {
		return "", e
	}
	port := c.server.Port
	if port == 0 {
		port = 445
	}
	return net.JoinHostPort(host, fmtPort(port)), nil
}

func (c *Client) smbAuth() (user, domain, password string) {
	user, domain = c.server.Username, c.server.Domain
	if user == "" {
		user = "guest"
	}
	return user, domain, c.server.Password
}

// withSMBSession dials and authenticates, but does not mount a share — used
// both for share enumeration (Share == "") and as the first step before
// mounting a specific share.
func (c *Client) withSMBSession(fn func(*smb2.Session) error) error {
	return c.withSMBSessionContext(context.Background(), fn)
}

func (c *Client) withSMBSessionContext(ctx context.Context, fn func(*smb2.Session) error) error {
	hostPort, e := c.smbHostPort()
	if e != nil {
		return e
	}
	conn, e := net.DialTimeout("tcp", hostPort, 15*time.Second)
	if e != nil {
		return errf(502, "Could not connect to SMB: %v", e)
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	user, domain, password := c.smbAuth()
	initiator := &smb2.NTLMInitiator{User: user, Password: password, Domain: domain}
	session, e := (&smb2.Dialer{Initiator: initiator}).Dial(conn)
	if e != nil {
		return errf(401, "SMB authentication failed: %v", e)
	}
	defer session.Logoff()
	return fn(session)
}

// withSMB mounts the configured Share and browses RootPath+segs — used by
// playback (Serve/ResolvePlayURL), where the source is always already fully
// smbTarget resolves the saved share/root pair, or (when the user deliberately
// left the source at the SMB host's top level) treats the first requested path
// component as the share. The latter mirrors listSMB: an unscoped SMB source
// first shows its share list, and a file chosen below one of those shares must
// remain playable without forcing the user to configure a root folder first.
func (c *Client) smbTarget(segs []string) (string, []string, error) {
	share := strings.TrimSpace(c.server.Share)
	if share == "" {
		if len(segs) == 0 {
			return "", nil, errf(400, "Select an SMB share first")
		}
		return segs[0], segs[1:], nil
	}
	return share, append(segments0(c.server.RootPath), segs...), nil
}

func (c *Client) withSMB(segs []string, fn func(*smb2.Share, string) error) error {
	return c.withSMBContext(context.Background(), segs, fn)
}

func (c *Client) withSMBContext(ctx context.Context, segs []string, fn func(*smb2.Share, string) error) error {
	share, pathSegs, err := c.smbTarget(segs)
	if err != nil {
		return err
	}
	path := filepath.ToSlash(filepath.Join(pathSegs...))
	return c.withSMBSessionContext(ctx, func(session *smb2.Session) error {
		mounted, e := session.Mount(share)
		if e != nil {
			return errf(502, "Could not mount SMB share: %v", e)
		}
		defer mounted.Umount()
		return fn(mounted, path)
	})
}

// listSMB has two modes depending on whether a share has already been
// chosen and persisted:
//   - Share == "" and segs empty: list the host's own shares (enumeration),
//     so the folder picker can drill into one exactly like any other
//     directory — this is what lets the user pick a share instead of typing
//     its name.
//   - Share == "" and segs non-empty: the browse path itself hasn't been
//     persisted yet (mid add-source flow), so segs[0] IS the share the user
//     already clicked into and segs[1:] is the sub-path within it.
//   - Share != "": the normal, already-configured case — browse
//     RootPath+segs within the stored share.
func (c *Client) listSMB(segs []string) (Listing, error) {
	share := strings.TrimSpace(c.server.Share)
	pathSegs := append(segments0(c.server.RootPath), segs...)
	if share == "" {
		if len(segs) == 0 {
			var names []string
			e := c.withSMBSession(func(session *smb2.Session) error {
				var e error
				names, e = session.ListSharenames()
				return e
			})
			if e != nil {
				return Listing{}, errf(502, "Could not list SMB shares: %v", e)
			}
			out := make([]Entry, 0, len(names))
			for _, name := range names {
				// IPC$/ADMIN$/C$ etc. are administrative shares, not media folders.
				if strings.HasSuffix(name, "$") {
					continue
				}
				out = append(out, entry(segs, name, true, 0))
			}
			return c.listing(segs, out), nil
		}
		share = segs[0]
		pathSegs = segs[1:]
	}
	out := []Entry{}
	path := filepath.ToSlash(filepath.Join(pathSegs...))
	e := c.withSMBSession(func(session *smb2.Session) error {
		mounted, e := session.Mount(share)
		if e != nil {
			return errf(502, "Could not mount SMB share: %v", e)
		}
		defer mounted.Umount()
		items, e := mounted.ReadDir(path)
		if e != nil {
			return e
		}
		for _, item := range items {
			dir := item.IsDir()
			if !dir && !isListedFile(item.Name()) {
				continue
			}
			out = append(out, entry(segs, item.Name(), dir, item.Size()))
		}
		return nil
	})
	if e != nil {
		return Listing{}, e
	}
	return c.listing(segs, out), nil
}

// ── playback ─────────────────────────────────────────────────────────────────

// ResolvePlayURL is a pre-flight existence/reachability check before
// playback starts (the caller discards the returned string and only uses
// the error — the actual URL handed to mpv is always the loopback
// /api/files/stream proxy, for every file kind uniformly). A failure here
// must stop playback before it's reported to Emby as started.
func (c *Client) ResolvePlayURL(path string) (string, error) {
	segs, e := segments(path)
	if e != nil {
		return "", e
	}
	switch c.kind() {
	case "local", "nfs":
		full, e := c.localPath(segs)
		if e != nil {
			return "", e
		}
		fsPath := osFilesystemPath(full)
		if _, e = os.Stat(fsPath); e != nil && fsPath != full {
			_, e = os.Stat(full)
		}
		if e != nil {
			return "", errf(404, "File not found")
		}
		return full, nil
	case "smb":
		e := c.withSMB(segs, func(share *smb2.Share, path string) error {
			f, e := share.Open(path)
			if e != nil {
				return errf(404, "File not found")
			}
			return f.Close()
		})
		if e != nil {
			return "", e
		}
		return path, nil
	default: // webdav
		u, e := c.remoteURL(segs)
		if e != nil {
			return "", e
		}
		parsed, _ := url.Parse(u)
		if c.server.Username != "" {
			parsed.User = url.UserPassword(c.server.Username, c.server.Password)
		}
		return parsed.String(), nil
	}
}

// Serve streams one configured file with HTTP Range support. Desktop playback
// uses this loopback proxy for SMB/WebDAV so it does not depend on the bundled
// mpv/FFmpeg build having a particular network protocol compiled in.
func (c *Client) Serve(w http.ResponseWriter, r *http.Request, path string) error {
	segs, err := segments(path)
	if err != nil {
		return err
	}
	switch c.kind() {
	case "local", "nfs":
		full, err := c.localPath(segs)
		if err != nil {
			return err
		}
		fsPath := osFilesystemPath(full)
		f, err := os.Open(fsPath)
		if err != nil && fsPath != full {
			f, err = os.Open(full)
		}
		if err != nil {
			return errf(404, "File not found")
		}
		defer f.Close()
		done := make(chan struct{})
		go func() {
			select {
			case <-r.Context().Done():
				_ = f.Close()
			case <-done:
			}
		}()
		defer close(done)
		info, err := f.Stat()
		if err != nil {
			return err
		}
		// Use the logical base name — extended paths can make Name() ugly.
		http.ServeContent(w, r, filepath.Base(full), info.ModTime(), f)
		return nil
	case "webdav":
		u, err := c.remoteURL(segs)
		if err != nil {
			return err
		}
		req, _ := http.NewRequestWithContext(r.Context(), "GET", u, nil)
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		if c.server.Username != "" {
			req.SetBasicAuth(c.server.Username, c.server.Password)
		}
		// Timeout: 0 is deliberate — a long playback stream must not be cut off
		// by a client-wide deadline; cancellation rides on the request context
		// (bound above), which fires when the player disconnects.
		resp, err := (&http.Client{Timeout: 0}).Do(req)
		if err != nil {
			return errf(502, "Could not stream WebDAV file: %v", err)
		}
		defer resp.Body.Close()
		for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
			if value := resp.Header.Get(key); value != "" {
				w.Header().Set(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		return err
	case "smb":
		return c.withSMBContext(r.Context(), segs, func(share *smb2.Share, path string) error {
			f, err := share.Open(path)
			if err != nil {
				return errf(404, "File not found")
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return err
			}
			http.ServeContent(w, r, info.Name(), info.ModTime(), f)
			return nil
		})
	default:
		return errf(400, "Unsupported file source type: %s", c.kind())
	}
}
