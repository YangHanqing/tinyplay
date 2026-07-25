package emby

// Server dialect: what this particular server actually accepts, learned at
// runtime instead of inferred from the user-declared source type.
//
// The Emby/Jellyfin ecosystem is not one protocol. Deployments differ in where
// the API is mounted (bare, or under /emby), which credential channel survives
// to the application (a reverse proxy may own Authorization), and which of the
// two spellings of an endpoint exists. Third-party NAS repackagings widen that
// further, and their advertised version numbers cannot be trusted.
//
// So `type` is treated as a label, not as a protocol switch: it only supplies
// the *initial guess* for the path prefix. Every request candidate that
// actually succeeds updates the profile below, and later requests start from
// what was learned. Nothing here is persisted — relearning costs one request,
// and a stale profile on disk would outlive the server change that invalidated
// it.
//
// The pure decision helpers (prefixCandidates/classify/authStylesFor) carry no
// state and no I/O so they can be exercised directly from dialect_test.go, and
// are mirrored in Swift by Core/MediaServerDialect.swift.

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tvremote/internal/config"
)

// authStyle is one credential channel. Emby historically reads
// X-Emby-Authorization, Jellyfin canonicalised on Authorization, and both
// accept an api_key query parameter — but a reverse proxy in front of either
// can consume Authorization for its own auth, so the working channel is a
// property of the deployment, not of the product.
type authStyle string

const (
	authBoth      authStyle = "both"          // default: send both headers
	authEmbyOnly  authStyle = "emby"          // proxy owns Authorization
	authCanonical authStyle = "authorization" // server rejects the legacy header
	authQuery     authStyle = "query"         // headers are mangled; ride the query string
)

// authLadder is the escalation order tried when a token is rejected. It runs
// only after a 401/403 and only while the working style is still unknown, so
// the healthy path stays at exactly one attempt.
var authLadder = []authStyle{authBoth, authEmbyOnly, authCanonical, authQuery}

// outcome is how one response maps onto the retry policy.
type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeUnauthorized
	outcomeMissingRoute
	outcomeBadShape
	outcomeServerError
)

// classify decides what a response means for the candidate ladder.
//
// The interesting case is outcomeBadShape: a reverse proxy that rewrites
// unknown paths to a single-page app answers an API call with 200 + HTML.
// Accepting that as success is what turns a misconfigured proxy into a silently
// empty library instead of a visible error, so a 2xx that is not JSON is
// treated as "this candidate is not the API" and the ladder moves on.
func classify(status int, body []byte) outcome {
	switch {
	case status == 401 || status == 403:
		return outcomeUnauthorized
	case status == 404 || status == 405 || status == 501:
		return outcomeMissingRoute
	case status >= 400:
		return outcomeServerError
	case len(bytes.TrimSpace(body)) == 0:
		return outcomeSuccess // 204 from the session-reporting endpoints
	case json.Valid(body):
		return outcomeSuccess
	default:
		return outcomeBadShape
	}
}

// prefixCandidates orders the API mount points to try, most likely first.
//
// bareFirst is the hint derived from the declared type (Jellyfin canonicalised
// on bare paths, Emby on /emby) and matters only until something is learned.
// A learned prefix is tried first but never exclusively: the alternative stays
// in the list so a wrong guess self-corrects on the next request rather than
// wedging the source.
func prefixCandidates(basePath, learned string, learnedKnown, bareFirst bool) []string {
	// A public URL that already ends in /emby (the common reverse-proxy shape)
	// would otherwise produce /emby/emby/... and 404 the whole source.
	base := strings.ToLower(strings.Trim(basePath, "/ "))
	if base == "emby" || strings.HasSuffix(base, "/emby") {
		return []string{""}
	}
	order := []string{"/emby", ""}
	if bareFirst {
		order = []string{"", "/emby"}
	}
	if !learnedKnown {
		return order
	}
	out := []string{learned}
	for _, p := range order {
		if p != learned {
			out = append(out, p)
		}
	}
	return out
}

// barePath strips an /emby prefix so a caller may pass either spelling.
func barePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path == "/emby" {
		return "/"
	}
	if strings.HasPrefix(path, "/emby/") {
		return strings.TrimPrefix(path, "/emby")
	}
	return path
}

func joinPrefix(prefix, path string) string { return prefix + barePath(path) }

// authStylesFor returns the styles to attempt for one request. Escalation is
// pointless without a token (an unauthenticated probe or a sign-in call proves
// nothing about credential channels) and once a style has been confirmed.
func authStylesFor(current authStyle, known, hasToken bool) []authStyle {
	if current == "" {
		current = authBoth
	}
	if known || !hasToken {
		return []authStyle{current}
	}
	styles := []authStyle{current}
	for _, s := range authLadder {
		if s != current {
			styles = append(styles, s)
		}
	}
	return styles
}

// ── learned profile cache ────────────────────────────────────────────────────

type dialect struct {
	baseURL     string
	prefix      string
	prefixKnown bool
	auth        authStyle
	authKnown   bool
	authToken   string // fingerprint of the token the auth verdict applies to
	product     string
	version     string
	probedAt    time.Time
}

// tokenFingerprint identifies a credential without retaining it, so a verdict
// about one token ("every channel rejected it") cannot be applied to the next
// one. Credentials reach config through several paths — sign-in, an address
// edit, a direct config write — and a rule that depended on all of them
// remembering to notify this package would eventually be broken by one of them.
func tokenFingerprint(token string) string {
	if token == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(token))
	return strconv.FormatUint(h.Sum64(), 16)
}

var (
	dialectMu    sync.Mutex
	dialectCache = map[string]*dialect{}
)

// dialectKey identifies a server across config reloads. Authenticate() runs
// against an unsaved candidate with no ID, so fall back to its address.
func dialectKey(s *config.Server) string {
	if s == nil {
		return ""
	}
	if id := strings.TrimSpace(s.ID); id != "" {
		return id
	}
	return "url:" + config.BuildServerURL(s)
}

// dialectFor returns a copy of the learned profile, resetting it when the
// server's address changed (a new address is a new deployment).
func dialectFor(s *config.Server) dialect {
	key := dialectKey(s)
	base := config.BuildServerURL(s)
	dialectMu.Lock()
	defer dialectMu.Unlock()
	d := dialectCache[key]
	if d == nil || d.baseURL != base {
		d = &dialect{baseURL: base, auth: authBoth}
		dialectCache[key] = d
	}
	return *d
}

func updateDialect(s *config.Server, mutate func(*dialect)) {
	key := dialectKey(s)
	base := config.BuildServerURL(s)
	dialectMu.Lock()
	defer dialectMu.Unlock()
	d := dialectCache[key]
	if d == nil || d.baseURL != base {
		d = &dialect{baseURL: base, auth: authBoth}
		dialectCache[key] = d
	}
	mutate(d)
}

// Nothing invalidates this cache by hand, and that is the point: an address
// change is caught by the baseURL comparison above, and a credential change by
// the token fingerprint on the auth verdict. An explicit "forget" entry point
// would be a fourth rule that every future caller has to remember — which is
// exactly how the ladder ended up pinned shut across a re-sign-in before.

// Identity is what a server reports about itself, for diagnostics and for
// showing the user the truth when the label they picked disagrees.
type Identity struct {
	Product    string
	ServerName string
	Version    string
	Prefix     string
	Auth       string
}

// bareFirstHint is the declared type's guess at the API mount point.
func (c *Client) bareFirstHint() bool {
	return c.server != nil && config.NormalizeServerType(c.server.Type) == "jellyfin"
}

func (c *Client) basePath() string {
	u, err := url.Parse(c.serverURL())
	if err != nil {
		return ""
	}
	return u.Path
}

func (c *Client) prefixes() []string {
	d := dialectFor(c.server)
	return prefixCandidates(c.basePath(), d.prefix, d.prefixKnown, c.bareFirstHint())
}

// preferredPath resolves a path for a URL handed to something that cannot
// retry — mpv, or an <img> tag. It uses the learned prefix when there is one
// and the declared type's guess otherwise.
func (c *Client) preferredPath(path string) string {
	prefixes := c.prefixes()
	if len(prefixes) == 0 {
		return barePath(path)
	}
	return joinPrefix(prefixes[0], path)
}

// learnPrefix records the mount point that answered. A prefix that contradicts
// the declared type is logged once: it is the fingerprint of a source added
// under the wrong type, and it is the single most useful line in a bug report
// about a "half working" server.
func (c *Client) learnPrefix(prefix string) {
	before := dialectFor(c.server)
	updateDialect(c.server, func(d *dialect) {
		d.prefix, d.prefixKnown = prefix, true
	})
	if before.prefixKnown && before.prefix == prefix {
		return
	}
	expected := "/emby"
	if c.bareFirstHint() {
		expected = ""
	}
	if prefix != expected && c.basePath() == "" {
		log.Printf("emby: server %q (declared %s) answers on %q, not %q — the source may be added under the wrong type",
			c.server.Name, config.NormalizeServerType(c.server.Type), prefixLabel(prefix), prefixLabel(expected))
	}
}

func prefixLabel(prefix string) string {
	if prefix == "" {
		return "bare paths"
	}
	return prefix
}

// learnAuth records the credential channel that was accepted *with a token*.
// A tokenless call (sign-in, public probe) proves nothing here and must not
// pin the style, or the ladder would never get to run. The verdict is tied to
// the token that earned it, so new credentials reopen the ladder on their own.
func (c *Client) learnAuth(style authStyle, token string) {
	updateDialect(c.server, func(d *dialect) {
		d.auth, d.authKnown, d.authToken = style, true, tokenFingerprint(token)
	})
}

// authSettled reports whether the cached auth verdict still applies to the
// credential now in hand.
func (d dialect) authSettled(token string) bool {
	return d.authKnown && d.authToken == tokenFingerprint(token)
}

// LearnedDialect reports the cached profile without touching the network, for
// diagnostics. Product/Version are populated only once a Probe has run.
func LearnedDialect(s *config.Server) Identity {
	d := dialectFor(s)
	prefix := "unlearned"
	if d.prefixKnown {
		prefix = prefixLabel(d.prefix)
	}
	auth := string(d.auth)
	if !d.authKnown {
		auth += " (unconfirmed)"
	}
	return Identity{Product: d.product, Version: d.version, Prefix: prefix, Auth: auth}
}

// ProbeInBackground records what a media server reports about itself so a bug
// report can show the truth next to the label the user picked. Failure is
// silent: this is diagnostics and must never gate adding or using a source.
func ProbeInBackground(s *config.Server) {
	if s == nil {
		return
	}
	switch config.NormalizeServerType(s.Type) {
	case "emby", "jellyfin":
	default:
		return
	}
	snapshot := *s
	go func() {
		id, err := New(&snapshot).Probe()
		if err != nil || id.Product == "" {
			return
		}
		log.Printf("emby: server %q (declared %s) reports %s %s", snapshot.Name,
			config.NormalizeServerType(snapshot.Type), id.Product, id.Version)
	}()
}

// Probe asks the server what it is and where its API lives.
//
// /System/Info/Public needs no token, so this answers even before sign-in and
// while credentials are wrong — which is exactly when a user needs to be told
// that the thing they labelled "Jellyfin" reports itself as Emby. Only what the
// server actually said is returned; no product is inferred from a version
// number, because repackaged builds report whatever they like.
func (c *Client) Probe() (Identity, error) {
	c.reload()
	if c.serverURL() == "" {
		return Identity{}, errf(400, "No server address is configured")
	}
	var lastErr error = errf(502, "The server did not answer /System/Info/Public")
	for _, prefix := range c.prefixes() {
		data, status, err := c.do("GET", joinPrefix(prefix, "/System/Info/Public"), nil, nil, authBoth)
		if err != nil {
			lastErr = err
			continue
		}
		if classify(status, data) != outcomeSuccess {
			lastErr = errf(status, "HTTP %d from %s", status, joinPrefix(prefix, "/System/Info/Public"))
			continue
		}
		var out struct {
			ProductName string `json:"ProductName"`
			ServerName  string `json:"ServerName"`
			Version     string `json:"Version"`
		}
		_ = json.Unmarshal(data, &out)
		c.learnPrefix(prefix)
		updateDialect(c.server, func(d *dialect) {
			d.product, d.version, d.probedAt = out.ProductName, out.Version, time.Now()
		})
		d := dialectFor(c.server)
		return Identity{
			Product:    out.ProductName,
			ServerName: out.ServerName,
			Version:    out.Version,
			Prefix:     prefixLabel(prefix),
			Auth:       string(d.auth),
		}, nil
	}
	return Identity{}, lastErr
}
