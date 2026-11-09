package server

import (
	"io/fs"
	"net/url"
	"strconv"

	"tvremote/internal/filesource"
	"tvremote/internal/player"
)

func playOpts(serverID, itemID, seriesID, seasonID, title, seriesTitle, episodeLabel, posterItemID string, startSeconds float64, mediaSourceID string) player.PlayOptions {
	return player.PlayOptions{
		ServerID:      serverID,
		ItemID:        itemID,
		SeriesID:      seriesID,
		SeasonID:      seasonID,
		Title:         title,
		SeriesTitle:   seriesTitle,
		EpisodeLabel:  episodeLabel,
		PosterItemID:  posterItemID,
		StartSeconds:  startSeconds,
		MediaSourceID: mediaSourceID,
	}
}

// applyFileAudioPresentation sets AudioOnly + CoverArtURL only when path is
// a genuine audio extension. Uncertain or video paths are left zero so mpv
// never gets cover art over a real video track.
func applyFileAudioPresentation(opts *player.PlayOptions, path string, port int) {
	if opts == nil || !filesource.IsAudio(path) {
		return
	}
	opts.AudioOnly = true
	opts.CoverArtURL = player.FallbackCoverArtURL(port)
}

// sidecarSubtitleURLs pairs the subtitle files beside a file-source video and
// returns them as loopback proxy URLs, the same way the video itself reaches
// mpv. Audio and pictures never get subtitles.
func (s *Server) sidecarSubtitleURLs(files *filesource.Client, serverID, path string) []string {
	if files == nil || filesource.IsAudio(path) || filesource.IsImage(path) {
		return nil
	}
	var urls []string
	for _, sub := range files.SidecarSubtitles(path) {
		urls = append(urls, s.fileStreamPlayURL(serverID, sub))
	}
	return urls
}

// fileStreamPlayURL builds the loopback streaming-proxy URL used for every
// file-source play (manual handler and host autoplay). Kept in one place so
// the two call sites cannot drift on query shape or host/port.
func (s *Server) fileStreamPlayURL(serverID, path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(s.port) +
		"/api/files/stream?server_id=" + url.QueryEscape(serverID) +
		"&path=" + url.QueryEscape(path)
}

func readWeb(s *Server, name string) ([]byte, error) {
	return fs.ReadFile(s.webFS, name)
}
