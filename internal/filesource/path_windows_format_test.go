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

// The drive-picker regression this pins: rebuilding the segment from its
// letter alone dropped the colon, so the browse base was the relative "C\"
// and every click into a drive failed as a bogus traversal.
func TestWindowsDriveRootBase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`C:`, `C:\`},
		{`c:`, `C:\`},
		{`C:/`, `C:\`},
		{`C:\`, `C:\`},
		{` C: `, `C:\`},
		{`Z:`, `Z:\`},
		// Not volumes: a folder that merely looks like one, and the
		// colon-less form that caused the bug.
		{`C`, ``},
		{`Media`, ``},
		{`C:Users`, ``},
		{``, ``},
	}
	for _, tc := range cases {
		got := windowsDriveRootBase(tc.in)
		if got != tc.want {
			t.Errorf("windowsDriveRootBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// A non-empty base must be usable as one: localPath relates request
		// segments to it with filepath.Rel, which cannot relate a relative
		// base to an absolute target.
		if got != "" && !isWindowsVolume(got[:len(got)-1]) {
			t.Errorf("windowsDriveRootBase(%q) = %q, which is not a volume root", tc.in, got)
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
