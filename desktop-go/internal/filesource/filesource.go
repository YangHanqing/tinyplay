// Package filesource browses and resolves local, SMB, and WebDAV sources.
package filesource

import (
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
	Size    int64  `json:"size"`
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
	if s == nil || config.NormalizeServerType(s.Type) != "file" {
		return nil, errf(400, "No file source is available")
	}
	return New(s), nil
}
func (c *Client) protocol() string {
	p := strings.ToLower(c.server.FileProtocol)
	if p == "" {
		p = "local"
	}
	return p
}

var videoExtensions = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".ts": true, ".m2ts": true, ".mts": true, ".mov": true, ".webm": true, ".flv": true, ".wmv": true, ".mpg": true, ".mpeg": true, ".iso": true, ".rmvb": true, ".rm": true, ".vob": true, ".ogv": true, ".divx": true, ".asf": true, ".3gp": true, ".f4v": true, ".mpv": true, ".dav": true}

func isVideo(name string) bool { return videoExtensions[strings.ToLower(filepath.Ext(name))] }
func segments(path string) ([]string, error) {
	out := []string{}
	for _, raw := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		s := strings.TrimSpace(raw)
		if s == "" || s == "." {
			continue
		}
		if s == ".." {
			return nil, errf(400, "Path traversal is not allowed")
		}
		out = append(out, s)
	}
	return out, nil
}
func (c *Client) listing(segs []string, entries []Entry) Listing {
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
	return Entry{name, strings.Join(append(append([]string{}, segs...), name), "/"), dir, !dir && isVideo(name), size}
}

func (c *Client) Verify() error {
	if strings.TrimSpace(c.server.Root) == "" {
		return errf(400, "A folder or share address is required")
	}
	_, err := c.ListDir("")
	return err
}
func (c *Client) ListDir(path string) (Listing, error) {
	segs, e := segments(path)
	if e != nil {
		return Listing{}, e
	}
	switch c.protocol() {
	case "local":
		return c.listLocal(segs)
	case "webdav":
		return c.listWebDAV(segs)
	case "smb":
		return c.listSMB(segs)
	case "nfs":
		return Listing{}, errf(400, "Mount NFS in the operating system and use a local folder")
	default:
		return Listing{}, errf(400, "Unsupported file protocol: %s", c.protocol())
	}
}

func (c *Client) localPath(segs []string) (string, error) {
	base, e := filepath.Abs(c.server.Root)
	if e != nil {
		return "", e
	}
	full, e := filepath.Abs(filepath.Join(append([]string{base}, segs...)...))
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(base, full)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errf(400, "Path traversal is not allowed")
	}
	return full, nil
}
func (c *Client) listLocal(segs []string) (Listing, error) {
	full, e := c.localPath(segs)
	if e != nil {
		return Listing{}, e
	}
	items, e := os.ReadDir(full)
	if e != nil {
		if os.IsNotExist(e) {
			return Listing{}, errf(404, "Folder not found")
		}
		return Listing{}, errf(502, "Could not list folder: %v", e)
	}
	out := []Entry{}
	for _, item := range items {
		dir := item.IsDir()
		if !dir && !isVideo(item.Name()) {
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
	u, e := url.Parse(strings.TrimRight(c.server.Root, "/"))
	if e != nil {
		return "", errf(400, "Invalid share address")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, s := range segs {
		parts = append(parts, url.PathEscape(s))
	}
	u.Path = "/" + strings.Join(parts, "/")
	return u.String(), nil
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
		if !dir && !isVideo(name) {
			continue
		}
		out = append(out, entry(segs, name, dir, size))
	}
	return c.listing(segs, out), nil
}

func (c *Client) smbParts(segs []string) (host, share, path string, err error) {
	u, e := url.Parse(c.server.Root)
	if e != nil || !strings.EqualFold(u.Scheme, "smb") {
		err = errf(400, "SMB address must look like smb://host/share")
		return
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Hostname() == "" || len(parts) < 1 || parts[0] == "" {
		err = errf(400, "SMB host and share are required")
		return
	}
	host = u.Hostname()
	if u.Port() != "" {
		host = net.JoinHostPort(host, u.Port())
	} else {
		host = net.JoinHostPort(host, "445")
	}
	share = parts[0]
	path = filepath.ToSlash(filepath.Join(append(parts[1:], segs...)...))
	return
}
func (c *Client) withSMB(segs []string, fn func(*smb2.Share, string) error) error {
	host, shareName, path, e := c.smbParts(segs)
	if e != nil {
		return e
	}
	conn, e := net.DialTimeout("tcp", host, 15*time.Second)
	if e != nil {
		return errf(502, "Could not connect to SMB: %v", e)
	}
	defer conn.Close()
	user, domain := c.server.Username, ""
	if user == "" {
		user = "guest"
	}
	if parts := strings.SplitN(user, "\\", 2); len(parts) == 2 {
		domain, user = parts[0], parts[1]
	}
	initiator := &smb2.NTLMInitiator{User: user, Password: c.server.Password, Domain: domain}
	session, e := (&smb2.Dialer{Initiator: initiator}).Dial(conn)
	if e != nil {
		return errf(401, "SMB authentication failed: %v", e)
	}
	defer session.Logoff()
	share, e := session.Mount(shareName)
	if e != nil {
		return errf(502, "Could not mount SMB share: %v", e)
	}
	defer share.Umount()
	return fn(share, path)
}
func (c *Client) listSMB(segs []string) (Listing, error) {
	out := []Entry{}
	e := c.withSMB(segs, func(share *smb2.Share, path string) error {
		items, e := share.ReadDir(path)
		if e != nil {
			return e
		}
		for _, item := range items {
			dir := item.IsDir()
			if !dir && !isVideo(item.Name()) {
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

func (c *Client) ResolvePlayURL(path string) (string, error) {
	segs, e := segments(path)
	if e != nil {
		return "", e
	}
	if c.protocol() == "local" {
		full, e := c.localPath(segs)
		if e != nil {
			return "", e
		}
		if _, e = os.Stat(full); e != nil {
			return "", errf(404, "File not found")
		}
		return full, nil
	}
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

// Serve streams one configured file with HTTP Range support. Desktop playback
// uses this loopback proxy for SMB/WebDAV so it does not depend on the bundled
// mpv/FFmpeg build having a particular network protocol compiled in.
func (c *Client) Serve(w http.ResponseWriter, r *http.Request, path string) error {
	segs, err := segments(path)
	if err != nil {
		return err
	}
	switch c.protocol() {
	case "local":
		full, err := c.localPath(segs)
		if err != nil {
			return err
		}
		f, err := os.Open(full)
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
		return c.withSMB(segs, func(share *smb2.Share, path string) error {
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
		return errf(400, "Unsupported file protocol: %s", c.protocol())
	}
}
