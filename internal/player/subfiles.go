package player

// External subtitle files for mpv.
//
// File-source videos reach mpv as a loopback proxy URL, so mpv's own sub-auto
// never sees the video's folder and a "Movie.srt" beside "Movie.mkv" was never
// loaded. The server pairs sidecar files itself (filesource.SidecarSubtitles)
// and passes them here as proxy URLs.
//
// The rules mirror cover art (audioart.go): set before loadfile, because mpv
// reads sub-files while opening the file; and set on every reused-process
// play, including to empty, or the previous film's subtitles would be loaded
// onto the next one.

// subtitleFileValue is the mpv sub-files list for a reused process. Never nil,
// so the JSON is [] (clear) rather than null.
func subtitleFileValue(urls []string) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// subtitleFileArgs returns startup flags for a fresh process. --sub-file
// appends one entry per flag; the list form (--sub-files) splits on the OS
// path separator, which a URL's ':' would break on macOS.
func subtitleFileArgs(urls []string) []string {
	var args []string
	for _, u := range subtitleFileValue(urls) {
		args = append(args, "--sub-file="+u)
	}
	return args
}

func (p *Player) applySubtitleFiles(urls []string) {
	p.send([]any{"set_property", "sub-files", subtitleFileValue(urls)})
}
