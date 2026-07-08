//go:build windows

package filesource

import "os"

// osRootBase resolves where to start browsing a local/nfs source that has no
// configured RootPath. Windows has no single root — at depth 0 the picker
// must choose a drive first (signaled by drivePicker), then subsequent
// segments resolve against "<drive>:\".
func osRootBase(segs []string) (base string, rest []string, drivePicker bool) {
	if len(segs) == 0 {
		return "", nil, true
	}
	return segs[0] + `\`, segs[1:], false
}

// driveEntries lists the drive letters that actually exist on this machine,
// presented as directory entries so the folder picker can browse into one
// exactly like any other folder.
func driveEntries() []Entry {
	out := []Entry{}
	for c := byte('A'); c <= 'Z'; c++ {
		letter := string(c)
		if _, err := os.Stat(letter + `:\`); err != nil {
			continue
		}
		out = append(out, Entry{Name: letter + ":", Path: letter + ":", IsDir: true})
	}
	return out
}
