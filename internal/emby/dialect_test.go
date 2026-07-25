package emby

import (
	"reflect"
	"testing"
)

func TestPrefixCandidates(t *testing.T) {
	cases := []struct {
		name      string
		basePath  string
		learned   string
		learnedOK bool
		bareFirst bool
		want      []string
	}{
		{name: "emby hint tries /emby first", want: []string{"/emby", ""}},
		{name: "jellyfin hint tries bare first", bareFirst: true, want: []string{"", "/emby"}},
		{
			// The classic reverse-proxy shape. Prefixing again yields
			// /emby/emby/... and 404s the whole source.
			name: "base path already ends in /emby", basePath: "/emby", want: []string{""},
		},
		{name: "nested base path ending in emby", basePath: "/media/emby", want: []string{""}},
		{
			// A learned value wins but never becomes exclusive: a stale guess
			// must be able to self-correct on the next request.
			name:    "learned bare still keeps the alternative",
			learned: "", learnedOK: true, want: []string{"", "/emby"},
		},
		{
			name:    "learned /emby overrides the jellyfin hint",
			learned: "/emby", learnedOK: true, bareFirst: true,
			want: []string{"/emby", ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := prefixCandidates(tc.basePath, tc.learned, tc.learnedOK, tc.bareFirst)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("prefixCandidates = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   outcome
	}{
		{"json object", 200, `{"Items":[]}`, outcomeSuccess},
		{"json array", 200, `[1,2]`, outcomeSuccess},
		{"empty 204 from session reporting", 204, "", outcomeSuccess},
		{"whitespace only", 200, "  \n", outcomeSuccess},
		// A proxy that rewrites unknown paths to its SPA answers 200 + HTML.
		// Treating that as success is what silently empties the library.
		{"proxy html", 200, "<!DOCTYPE html><html><body>app</body></html>", outcomeBadShape},
		{"plain text", 200, "Not Found", outcomeBadShape},
		{"unauthorized", 401, `{"error":"no"}`, outcomeUnauthorized},
		{"forbidden", 403, "", outcomeUnauthorized},
		{"missing route", 404, "", outcomeMissingRoute},
		{"method not allowed", 405, "", outcomeMissingRoute},
		{"not implemented", 501, "", outcomeMissingRoute},
		{"server error", 500, "", outcomeServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("classify(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestAuthStylesFor(t *testing.T) {
	if got := authStylesFor(authBoth, false, false); !reflect.DeepEqual(got, []authStyle{authBoth}) {
		t.Fatalf("tokenless call must not escalate: %v", got)
	}
	if got := authStylesFor(authEmbyOnly, true, true); !reflect.DeepEqual(got, []authStyle{authEmbyOnly}) {
		t.Fatalf("a confirmed style must not escalate: %v", got)
	}
	got := authStylesFor(authBoth, false, true)
	if len(got) != len(authLadder) || got[0] != authBoth {
		t.Fatalf("full ladder expected, got %v", got)
	}
	seen := map[authStyle]bool{}
	for _, s := range got {
		if seen[s] {
			t.Fatalf("ladder repeats %q: %v", s, got)
		}
		seen[s] = true
	}
	// A confirmed style leads the ladder when it is re-opened after re-login.
	if got := authStylesFor(authQuery, false, true); got[0] != authQuery {
		t.Fatalf("ladder must start from the current style, got %v", got)
	}
}

// Pinning the credential channel is only safe when every channel was actually
// rejected. A network failure or a 404 proves nothing about credentials, and
// treating it as proof would disable the ladder for the rest of the session.
func TestEscalationExhausted(t *testing.T) {
	full := authStylesFor(authBoth, false, true)
	if !escalationExhausted(full, true) {
		t.Fatal("a fully rejected ladder is exhausted")
	}
	if escalationExhausted(full, false) {
		t.Fatal("a ladder abandoned on a non-auth failure is not exhausted")
	}
	if escalationExhausted([]authStyle{authBoth}, true) {
		t.Fatal("a single-style attempt never exhausts the ladder")
	}
}

func TestBarePathAndJoin(t *testing.T) {
	cases := map[string]string{
		"/Users/x/Items":      "/Users/x/Items",
		"Users/x/Items":       "/Users/x/Items",
		"/emby/Users/x/Items": "/Users/x/Items",
		"/emby":               "/",
	}
	for in, want := range cases {
		if got := barePath(in); got != want {
			t.Fatalf("barePath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := joinPrefix("/emby", "/emby/Users/x"); got != "/emby/Users/x" {
		t.Fatalf("joinPrefix must not double the prefix, got %q", got)
	}
	if got := joinPrefix("", "/emby/Users/x"); got != "/Users/x" {
		t.Fatalf("joinPrefix = %q", got)
	}
}
