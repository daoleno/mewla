package server

import (
	"encoding/json"
	"errors"
	"net/http"
)

// DaemonUpdateStatus is what the app shows about this computer's Mewla.
type DaemonUpdateStatus struct {
	// Version is the Mewla running on this computer.
	Version string `json:"version"`
	// Supported is false for builds that do not update themselves (a
	// development build, a beta, macOS until it is verified).
	Supported bool `json:"supported"`
	// Latest is the newer stable release, when there is one.
	Latest string `json:"latest,omitempty"`
	// Updating is true while the computer downloads and restarts into Latest.
	Updating bool `json:"updating,omitempty"`
	// Error is why the last update did not take; the previous version kept
	// running.
	Error string `json:"error,omitempty"`
}

// DaemonUpdater is the computer's self-update.
type DaemonUpdater interface {
	Status() DaemonUpdateStatus
	// Install starts updating now and returns once it has begun; the daemon
	// restarts shortly after.
	Install() error
}

// SetDaemonUpdater installs the computer's self-update.
func (s *Server) SetDaemonUpdater(updater DaemonUpdater) { s.daemonUpdater = updater }

// SetVersion sets the Mewla version /health reports.
func (s *Server) SetVersion(version string) {
	s.version = version
	s.hostTerminal.Version = version
}

// ErrUpdateUnavailable: nothing to install, or this build does not update.
var ErrUpdateUnavailable = errors.New("no update to install")

func (s *Server) handleDaemonUpdate(w http.ResponseWriter, r *http.Request) {
	if s.daemonUpdater == nil {
		http.Error(w, "update unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-daemon-update:GET:/daemon-update"); !ok {
			return
		}
	case http.MethodPost:
		if _, ok := s.authenticateRequest(w, r, "mewla-daemon-update:POST:/daemon-update"); !ok {
			return
		}
		if err := s.daemonUpdater.Install(); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrUpdateUnavailable) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.daemonUpdater.Status())
}
