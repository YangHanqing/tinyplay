package server

import (
	"net/http"

	"tvremote/internal/config"
	"tvremote/internal/filesource"
	"tvremote/internal/provider"
)

// filesClient resolves the request's source to a file-source client
// (local/SMB/WebDAV/NFS), honoring ?server_id= before the active source. The
// folder picker relies on server_id to browse a just-created, not-yet-active
// source.
func filesClient(r *http.Request) (*filesource.Client, error) {
	// Local/NFS add flows browse the host filesystem before a source exists.
	// Keep that preview deliberately limited to the two networkless kinds;
	// SMB/WebDAV still require a persisted host/credential candidate.
	if r.URL.Query().Get("server_id") == "" {
		kind := config.NormalizeServerType(r.URL.Query().Get("source_type"))
		if kind == "local" || kind == "nfs" {
			return filesource.New(&config.Server{Name: kind, Type: kind}), nil
		}
	}
	return clientForRequest(r, provider.FileFromServer, provider.ActiveFile)
}

func (s *Server) filesList(w http.ResponseWriter, r *http.Request) {
	c, err := filesClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	listing, err := c.ListDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) filesStream(w http.ResponseWriter, r *http.Request) {
	c, err := filesClient(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := c.Serve(w, r, r.URL.Query().Get("path")); err != nil {
		writeErr(w, r, err)
	}
}
