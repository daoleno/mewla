package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
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
	// WaitingFor is what a staged update waits for before it restarts the
	// daemon ("a permission answer"); empty when it is not waiting.
	WaitingFor string `json:"waiting_for,omitempty"`
	// InstallBy is when a waiting update installs anyway.
	InstallBy *time.Time `json:"install_by,omitempty"`
	// Error is why the last update did not take; the previous version kept
	// running.
	Error string `json:"error,omitempty"`
	// HostVersion is the session host's version: it keeps running across
	// daemon updates, so it can be older than Version.
	HostVersion string `json:"host_version,omitempty"`
	// TerminalsRestart: the session host is too old for this Mewla and
	// restarts on its own only once that ends nothing; restarting the
	// terminals now (RestartTerminals) finishes the update.
	TerminalsRestart bool `json:"terminals_restart,omitempty"`
	// LiveTerminals is how many terminals restarting them now would end.
	LiveTerminals int `json:"live_terminals,omitempty"`
}

// DaemonUpdater is the computer's self-update.
type DaemonUpdater interface {
	Status() DaemonUpdateStatus
	// Install starts updating now and returns once it has begun; the daemon
	// restarts shortly after.
	Install() error
	// RestartTerminals restarts the session host now, ending every terminal
	// in it; Brain comes back in its conversation.
	RestartTerminals() error
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
	status := s.daemonUpdater.Status()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(status)
}

// handleRestartTerminals is the app's "Restart terminals" tap.
func (s *Server) handleRestartTerminals(w http.ResponseWriter, r *http.Request) {
	if s.daemonUpdater == nil {
		http.Error(w, "update unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authenticateRequest(w, r, "mewla-daemon-update:POST:/daemon-update/terminals"); !ok {
		return
	}
	if err := s.daemonUpdater.RestartTerminals(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.daemonUpdater.Status())
}
