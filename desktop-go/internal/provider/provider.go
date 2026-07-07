// Package provider dispatches the shared desktop API to Emby, Jellyfin, Plex,
// or a file source without coupling those clients to the HTTP server/player.
package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"tvremote/internal/config"
	"tvremote/internal/emby"
	"tvremote/internal/filesource"
	"tvremote/internal/iptv"
	"tvremote/internal/plex"
)

type PlayChoice struct{ URL, MediaSourceID string }

type Media interface {
	Kind() string
	Libraries() ([]byte, error)
	Items(parent, search string, start, limit int, includeEpisodes bool) ([]byte, error)
	Resume(limit int) ([]byte, error)
	ItemDetailRaw(id string) ([]byte, error)
	Episodes(series, season string, start, limit int, sort string) ([]byte, error)
	Seasons(seriesID string) ([]byte, error)
	ImageBytes(id string, maxHeight int, imageType string) ([]byte, string)
	BackdropBytes(id string, maxHeight, index int) ([]byte, string)
	ChoosePlayURL(id string) (PlayChoice, error)
	ResumePositionSeconds(id string) float64
	ReportStart(id, session, source string)
	ReportProgress(id, session string, ticks int64, paused bool, source string)
	ReportStopped(id, session string, ticks int64, source string)
}

type embyMedia struct {
	c    *emby.Client
	kind string
}

func (m *embyMedia) Kind() string               { return m.kind }
func (m *embyMedia) Libraries() ([]byte, error) { return m.c.Libraries() }
func (m *embyMedia) Items(a, b string, c, d int, e bool) ([]byte, error) {
	return m.c.Items(a, b, c, d, e)
}
func (m *embyMedia) Resume(a int) ([]byte, error)           { return m.c.Resume(a) }
func (m *embyMedia) ItemDetailRaw(a string) ([]byte, error) { return m.c.ItemDetailRaw(a) }
func (m *embyMedia) Episodes(a, b string, c, d int, e string) ([]byte, error) {
	return m.c.Episodes(a, b, c, d, e)
}
func (m *embyMedia) Seasons(a string) ([]byte, error) { return m.c.Seasons(a) }
func (m *embyMedia) ImageBytes(a string, b int, c string) ([]byte, string) {
	return m.c.ImageBytes(a, b, c)
}
func (m *embyMedia) BackdropBytes(a string, b, c int) ([]byte, string) {
	return m.c.BackdropBytes(a, b, c)
}
func (m *embyMedia) ChoosePlayURL(a string) (PlayChoice, error) {
	x, e := m.c.ChoosePlayURL(a)
	return PlayChoice{x.URL, x.MediaSourceID}, e
}
func (m *embyMedia) ResumePositionSeconds(a string) float64 { return m.c.ResumePositionSeconds(a) }
func (m *embyMedia) ReportStart(a, b, c string)             { m.c.ReportStart(a, b, c) }
func (m *embyMedia) ReportProgress(a, b string, c int64, d bool, e string) {
	m.c.ReportProgress(a, b, c, d, e)
}
func (m *embyMedia) ReportStopped(a, b string, c int64, d string) { m.c.ReportStopped(a, b, c, d) }

type plexMedia struct{ c *plex.Client }

func (m *plexMedia) Kind() string               { return "plex" }
func (m *plexMedia) Libraries() ([]byte, error) { return m.c.Libraries() }
func (m *plexMedia) Items(a, b string, c, d int, e bool) ([]byte, error) {
	return m.c.Items(a, b, c, d, e)
}
func (m *plexMedia) Resume(a int) ([]byte, error)           { return m.c.Resume(a) }
func (m *plexMedia) ItemDetailRaw(a string) ([]byte, error) { return m.c.ItemDetailRaw(a) }
func (m *plexMedia) Episodes(a, b string, c, d int, e string) ([]byte, error) {
	return m.c.Episodes(a, b, c, d, e)
}
func (m *plexMedia) Seasons(a string) ([]byte, error) { return m.c.Seasons(a) }
func (m *plexMedia) ImageBytes(a string, b int, c string) ([]byte, string) {
	return m.c.ImageBytes(a, b, c)
}
func (m *plexMedia) BackdropBytes(a string, b, c int) ([]byte, string) {
	return m.c.BackdropBytes(a, b, c)
}
func (m *plexMedia) ChoosePlayURL(a string) (PlayChoice, error) {
	x, e := m.c.ChoosePlayURL(a)
	return PlayChoice{x.URL, x.MediaSourceID}, e
}
func (m *plexMedia) ResumePositionSeconds(a string) float64 { return m.c.ResumePositionSeconds(a) }
func (m *plexMedia) ReportStart(a, b, c string)             { m.c.ReportStart(a, b, c) }
func (m *plexMedia) ReportProgress(a, b string, c int64, d bool, e string) {
	m.c.ReportProgress(a, b, c, d, e)
}
func (m *plexMedia) ReportStopped(a, b string, c int64, d string) { m.c.ReportStopped(a, b, c, d) }

func Active() (Media, error) {
	s := config.ActiveServer()
	if s == nil {
		return nil, &APIError{400, "No media source is available. Add one first."}
	}
	switch config.NormalizeServerType(s.Type) {
	case "plex":
		return &plexMedia{plex.New(s)}, nil
	case "file":
		return nil, &APIError{400, "The active source is a file source"}
	case "iptv":
		return nil, &APIError{400, "The active source is an IPTV source"}
	default:
		return &embyMedia{emby.New(s), config.NormalizeServerType(s.Type)}, nil
	}
}

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string   { return e.Msg }
func (e *APIError) StatusCode() int { return e.Status }

func Authenticate(s *config.Server, username, password, token string) (string, string, error) {
	switch config.NormalizeServerType(s.Type) {
	case "plex":
		return plex.Authenticate(s, username, password, token)
	case "file":
		if err := filesource.New(s).Verify(); err != nil {
			return "", "", err
		}
		return "", "", nil
	case "iptv":
		// The real playlist/EPG fetch happens after AddServer assigns a
		// server id (see handlers.createServer) since the iptv package
		// caches per-id — this only validates the required field up front.
		if strings.TrimSpace(s.PlaylistURL) == "" {
			return "", "", &APIError{400, "A playlist URL is required"}
		}
		return "", "", nil
	default:
		return emby.Authenticate(s, username, password)
	}
}
func VerifyFile(s *config.Server) error       { return filesource.New(s).Verify() }
func ActiveFile() (*filesource.Client, error) { return filesource.FromActive() }
func ActiveIPTV() (*iptv.Client, error)       { return iptv.FromActive() }

func EmptyItems() []byte {
	b, _ := json.Marshal(map[string]any{"Items": []any{}, "TotalRecordCount": 0})
	return b
}
func Errorf(status int, format string, args ...any) error {
	return &APIError{status, fmt.Sprintf(format, args...)}
}
