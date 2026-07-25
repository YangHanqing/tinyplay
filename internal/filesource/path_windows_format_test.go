package filesource

import "testing"

func TestFormatWindowsFilesystemPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`Z:\Media\Movies`, `\\?\Z:\Media\Movies`},
		{`Z:/Media/Movies`, `\\?\Z:\Media\Movies`},
		{`Z:`, `\\?\Z:\`},
		{`Z:\`, `\\?\Z:\`},
		{`\\nas\share\Movies`, `\\?\UNC\nas\share\Movies`},
		{`\\?\Z:\already`, `\\?\Z:\already`},
		{`C:\very\long\` + stringsRepeat(`a`, 200), `\\?\C:\very\long\` + stringsRepeat(`a`, 200)},
	}
	for _, tc := range cases {
		if got := formatWindowsFilesystemPath(tc.in); got != tc.want {
			t.Errorf("formatWindowsFilesystemPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeWindowsConfiguredRoot(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`Z:`, `Z:\`},
		{`z:/`, `Z:\`},
		{`Z:\`, `Z:\`},
		{`Z:/Media`, `Z:/Media`},
		{`Z:\Media`, `Z:\Media`},
		{``, ``},
	}
	for _, tc := range cases {
		if got := normalizeWindowsConfiguredRoot(tc.in); got != tc.want {
			t.Errorf("normalizeWindowsConfiguredRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
