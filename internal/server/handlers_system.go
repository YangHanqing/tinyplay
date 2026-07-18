package server

import (
	"net/http"

	"tvremote/internal/sysvolume"
)

func (s *Server) systemVolumeGet(w http.ResponseWriter, r *http.Request) {
	vol, err := sysvolume.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	muted, err := sysvolume.GetMuted()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"volume": vol, "muted": muted})
}

func (s *Server) systemVolumeSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Volume *int  `json:"volume"`
		Muted  *bool `json:"muted"`
	}
	if !decode(r, &req) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid body"})
		return
	}
	result := map[string]any{}
	if req.Volume != nil {
		vol, err := sysvolume.Set(*req.Volume)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
			return
		}
		result["volume"] = vol
	}
	if req.Muted != nil {
		muted, err := sysvolume.SetMuted(*req.Muted)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
			return
		}
		result["muted"] = muted
	}
	writeJSON(w, http.StatusOK, result)
}
