package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/computers"
)

// SetComputers installs the channel other Mewla computers use to reach this
// one's Brain.
// SetDeviceHook hears the phone's timezone and language whenever it registers for push, so
// time-of-day and language features follow the person and not the computer.
func (s *Server) SetDeviceHook(hook func(timeZone, locale string)) { s.deviceHook = hook }

func (s *Server) SetComputers(manager *computers.Manager) { s.computers = manager }

// SetComputerName is what people call this computer; a paired device reads
// it from /auth-check and shows it instead of an address.
func (s *Server) SetComputerName(name string) { s.computerName = name }

// handleComputer is the only route a computer-kind device may use. The auth
// manager refuses its key on every other purpose.
func (s *Server) handleComputer(w http.ResponseWriter, r *http.Request) {
	if s.computers == nil {
		http.Error(w, "computers unavailable", http.StatusServiceUnavailable)
		return
	}
	device, ok := s.authenticateRequest(w, r, auth.ComputerPurpose)
	if !ok {
		return
	}
	s.computers.ServeInbound(w, r, device)
}

// handleComputersInvite lets a paired phone ask this computer for a one-time
// invite, to hand to another computer it also trusts.
func (s *Server) handleComputersInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.computers == nil {
		http.Error(w, "computers unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.authenticateRequest(w, r, "mewla-computer-admin:invite:POST:/computers/invite"); !ok {
		return
	}
	result, err := s.computers.Handle(r.Context(), computers.Request{Action: "invite"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	expires := ""
	if result.ExpiresAt != nil {
		expires = result.ExpiresAt.Format(time.RFC3339)
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "mewla-computer-invite", map[string]any{
		"invite":     result.Invite,
		"daemon_id":  s.auth.DaemonID(),
		"expires_at": expires,
	})
}

// handleComputers lists, adds and removes the computers this one works with.
// Add and remove bind their signature to the request body.
func (s *Server) handleComputers(w http.ResponseWriter, r *http.Request) {
	if s.computers == nil {
		http.Error(w, "computers unavailable", http.StatusServiceUnavailable)
		return
	}
	var req computers.Request
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-computer-admin:list:GET:/computers"); !ok {
			return
		}
		req.Action = "list"
	case http.MethodPost, http.MethodDelete:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		action := "add"
		if r.Method == http.MethodDelete {
			action = "remove"
		}
		purpose := "mewla-computer-admin:" + action + ":" + r.Method + ":/computers:sha256=" + hex.EncodeToString(sum[:])
		if _, ok := s.authenticateRequest(w, r, purpose); !ok {
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Action = action
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result, err := s.computers.Handle(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "could not reach") {
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
