package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
)

// WatchSwitch is Watch's one setting the app shows (Settings › Privacy & access, "Notice what changes in your
// projects"): whether Watch looks over the projects. The CLI's `mewla watch enable|disable` changes the same.
type WatchSwitch interface {
	Enabled() bool
	SetEnabled(enabled bool) (bool, error)
}

// SetWatch installs Watch's switch; without it /watch answers 503 (no Watch on this computer).
func (s *Server) SetWatch(watch WatchSwitch) { s.watchSwitch = watch }

// watchSetting is what /watch reads and sets: nothing more than the switch.
type watchSetting struct {
	Enabled bool `json:"enabled"`
}

// handleWatch reads Watch's switch (GET) or sets it (POST {"enabled": bool}). A change binds its signature to the
// body, like /agent-use. A daemon older than this endpoint answers 404, and the app hides the row.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	if s.watchSwitch == nil {
		http.Error(w, "watch unavailable", http.StatusServiceUnavailable)
		return
	}
	var setting watchSetting
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-watch:GET:/watch"); !ok {
			return
		}
		setting.Enabled = s.watchSwitch.Enabled()
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-watch:POST:/watch:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var change struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.Unmarshal(body, &change); err != nil || change.Enabled == nil {
			http.Error(w, "send enabled", http.StatusBadRequest)
			return
		}
		if setting.Enabled, err = s.watchSwitch.SetEnabled(*change.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(setting)
}
