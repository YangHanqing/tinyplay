package emby

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"tvremote/internal/config"
)

// The Emby/Jellyfin field is not one protocol, and we cannot test against every
// NAS repackaging that exists. What we can do is turn every deployment shape we
// have actually met into a fixture here — mount point, credential channel, and
// reverse-proxy behaviour — and hold the client to all of them at once.
//
// Add a fixture whenever a new server in the wild behaves differently.
type fixture struct {
	acceptPrefixed bool   // serves /emby/...
	acceptBare     bool   // serves /...
	authHeader     string // required header; "" means the api_key query only
	spaFallback    bool   // unknown paths answer 200 + HTML instead of 404
	proxyOwnsAuth  bool   // a reverse proxy consumes Authorization and refuses requests carrying it

	mu       sync.Mutex
	requests []string
}

const fixtureToken = "tok-1"
const fixtureUser = "user-1"

func (f *fixture) log(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, path)
}

func (f *fixture) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fixture) count() int { return len(f.seen()) }

func (f *fixture) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

func (f *fixture) authorized(r *http.Request) bool {
	// A proxy that owns Authorization refuses anything carrying it, which is
	// what makes the default both-headers style fail. Without this shape the
	// middle rungs of the ladder are never exercised: a server that merely
	// ignores the header it does not use lets "both" win every time.
	if f.proxyOwnsAuth && r.Header.Get("Authorization") != "" {
		return false
	}
	if f.authHeader == "" {
		return r.URL.Query().Get("api_key") == fixtureToken
	}
	return strings.Contains(r.Header.Get(f.authHeader), fixtureToken)
}

func (f *fixture) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.log(r.URL.Path)
		route := r.URL.Path
		prefixed := strings.HasPrefix(route, "/emby/") || route == "/emby"
		if prefixed {
			route = strings.TrimPrefix(route, "/emby")
		}
		mounted := (prefixed && f.acceptPrefixed) || (!prefixed && f.acceptBare)
		if !mounted {
			if f.spaFallback {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("<!DOCTYPE html><html><body>web app</body></html>"))
				return
			}
			http.NotFound(w, r)
			return
		}

		if route == "/System/Info/Public" { // unauthenticated by design
			writeJSON(w, map[string]any{"ProductName": "Emby Server", "Version": "4.8.11.0", "ServerName": "NAS"})
			return
		}
		if route == "/Users/AuthenticateByName" {
			writeJSON(w, map[string]any{"AccessToken": fixtureToken, "User": map[string]any{"Id": fixtureUser}})
			return
		}
		if !f.authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		writeJSON(w, map[string]any{"Items": []any{}, "TotalRecordCount": 0})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// newFixtureClient saves a server pointing at the fixture and returns a client.
func newFixtureClient(t *testing.T, f *fixture, kind, basePath string) *Client {
	t.Helper()
	srv := f.start(t)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	saved := config.AddServer(config.Server{
		Name: "fixture", Type: kind, Protocol: u.Scheme,
		Hosts: []string{u.Hostname()}, Port: port, BasePath: basePath,
		UserID: fixtureUser, AccessToken: fixtureToken,
	})
	return New(saved)
}

// An Emby server added under the Jellyfin type: the wrong label must not change
// what the client can read, and the working mount point must be learned so the
// second call costs one request instead of two.
func TestEmbyServerAddedAsJellyfinStillWorks(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "jellyfin", "")

	if _, err := c.Libraries(); err != nil {
		t.Fatalf("libraries failed on a mislabelled source: %v", err)
	}
	f.reset()
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if n := f.count(); n != 1 {
		t.Fatalf("learned dialect should cost 1 request, got %d: %v", n, f.seen())
	}
}

// The mirror image: a Jellyfin-shaped server (bare mount, canonical header only)
// added under the Emby type.
func TestJellyfinServerAddedAsEmbyStillWorks(t *testing.T) {
	f := &fixture{acceptBare: true, authHeader: "Authorization"}
	c := newFixtureClient(t, f, "emby", "")

	if _, err := c.Libraries(); err != nil {
		t.Fatalf("libraries failed on a mislabelled source: %v", err)
	}
	f.reset()
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	seen := f.seen()
	if len(seen) != 1 || strings.HasPrefix(seen[0], "/emby") {
		t.Fatalf("expected one bare request after learning, got %v", seen)
	}
}

// A reverse proxy that rewrites unknown paths to its single-page app answers an
// API call with 200 + HTML. That must never be reported as success.
func TestProxyHTMLIsNotMistakenForData(t *testing.T) {
	f := &fixture{acceptPrefixed: true, spaFallback: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "jellyfin", "") // bare-first hint hits the HTML page

	data, err := c.Libraries()
	if err != nil {
		t.Fatalf("libraries should have fallen through to /emby: %v", err)
	}
	if !json.Valid(data) || strings.Contains(string(data), "<html") {
		t.Fatalf("HTML leaked to the caller: %q", data)
	}
}

func TestEveryPathSwallowedByProxyIsAnError(t *testing.T) {
	f := &fixture{spaFallback: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "emby", "")

	data, err := c.Libraries()
	if err == nil {
		t.Fatalf("a proxy swallowing the whole API must be an error, got %q", data)
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.Status != 502 {
		t.Fatalf("want a 502 explaining the proxy, got %v", err)
	}
}

// A public URL that already ends in /emby is the common reverse-proxy shape.
// Adding the prefix again produces /emby/emby/... and 404s the whole source.
func TestSubPathDeploymentNeverDoublesThePrefix(t *testing.T) {
	f := &fixture{acceptPrefixed: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "emby", "emby")

	if _, err := c.Libraries(); err != nil {
		t.Fatalf("libraries failed behind a /emby base path: %v", err)
	}
	for _, path := range f.seen() {
		if strings.HasPrefix(path, "/emby/emby") {
			t.Fatalf("doubled prefix: %v", f.seen())
		}
	}
}

// Some deployments put their own auth on Authorization, or strip headers
// entirely; the token then only survives in the query string. The ladder must
// find that channel, and must stop paying for it once it has.
func TestAuthLadderFindsTheQueryOnlyChannel(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true, authHeader: ""}
	c := newFixtureClient(t, f, "emby", "")

	if _, err := c.Libraries(); err != nil {
		t.Fatalf("the credential ladder did not find the query channel: %v", err)
	}
	f.reset()
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if n := f.count(); n != 1 {
		t.Fatalf("a confirmed channel should cost 1 request, got %d: %v", n, f.seen())
	}
}

// A genuinely bad token exhausts the ladder once, then fails fast: the UI fires
// many calls, and each must not re-run four credential channels.
func TestRejectedTokenStopsEscalatingAfterTheFirstFailure(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "emby", "")
	config.SetAuth(c.server.ID, "u", "wrong-token", fixtureUser)

	if _, err := c.Libraries(); err == nil {
		t.Fatal("a rejected token must surface as an error")
	}
	f.reset()
	if _, err := c.Libraries(); err == nil {
		t.Fatal("a rejected token must surface as an error")
	}
	if n := f.count(); n > 2 {
		t.Fatalf("ladder re-ran on a known-bad token: %d requests %v", n, f.seen())
	}
}

// The "stop escalating" verdict belongs to the token that earned it. New
// credentials must reopen the ladder no matter which path stored them — the
// sign-in handler, an address edit, and a direct config write all persist a
// token without notifying this package — or a source whose server needs a
// non-default channel would stay broken after a correct re-sign-in.
func TestFreshCredentialsReopenTheLadder(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true, authHeader: ""} // query-only server
	c := newFixtureClient(t, f, "emby", "")
	config.SetAuth(c.server.ID, "u", "stale-token", fixtureUser)

	if _, err := c.Libraries(); err == nil {
		t.Fatal("a rejected token must surface as an error")
	}
	// The address-edit path: it persists a token without going through this
	// package at all, so nothing here is told the credential changed.
	config.SetAuth(c.server.ID, "u", fixtureToken, fixtureUser)
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("a fresh, valid token must work again: %v", err)
	}
}

// The shape the narrow credential channels exist for: a reverse proxy holding
// Authorization for its own auth, in front of a server that reads
// X-Emby-Authorization. The default both-headers style is refused by the proxy,
// so the ladder has to fall through to the narrower one — and after a stale
// token has exhausted every channel, a correct re-sign-in must still get there.
func TestProxyOwningAuthorizationIsWorkedAround(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true,
		authHeader: "X-Emby-Authorization", proxyOwnsAuth: true}
	c := newFixtureClient(t, f, "emby", "")
	config.SetAuth(c.server.ID, "u", "stale-token", fixtureUser)

	if _, err := c.Libraries(); err == nil {
		t.Fatal("a rejected token must surface as an error")
	}
	config.SetAuth(c.server.ID, "u", fixtureToken, fixtureUser)
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("the ladder must reopen and find the narrow channel: %v", err)
	}
	f.reset()
	if _, err := c.Libraries(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if n := f.count(); n != 1 {
		t.Fatalf("a confirmed channel should cost 1 request, got %d: %v", n, f.seen())
	}
}

func TestProbeReportsWhatTheServerSaysItIs(t *testing.T) {
	f := &fixture{acceptPrefixed: true, acceptBare: true, authHeader: "X-Emby-Authorization"}
	c := newFixtureClient(t, f, "jellyfin", "")

	id, err := c.Probe()
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if id.Product != "Emby Server" || id.Version != "4.8.11.0" {
		t.Fatalf("probe = %+v", id)
	}
}

func asAPIError(err error, out **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*out = e
	}
	return ok
}
