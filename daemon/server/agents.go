package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/daoleno/mewla/daemon/outsideagents"
)

// SetOutsideAgents turns on the MCP door for outside agents (/mcp) and the
// paired phone's view of them (/agents).
func (s *Server) SetOutsideAgents(manager *outsideagents.Manager, door http.Handler) {
	s.outsideAgents, s.outsideAgentsDoor = manager, door
}

// handleMCP serves the door. It is not in the private-listener allowlist, and
// the door itself refuses anything but this computer and TLS (see
// outsideagents.TransportAllowed).
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.outsideAgentsDoor == nil {
		http.NotFound(w, r)
		return
	}
	s.outsideAgentsDoor.ServeHTTP(w, r)
}

type agentView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ApprovedAt time.Time  `json:"approved_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type pendingAgentView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Number    string    `json:"number"`
	ExpiresAt time.Time `json:"expires_at"`
}

type agentsDecision struct {
	Action string `json:"action"` // approve, deny or revoke
	ID     string `json:"id"`
}

// handleAgents lists the outside agents waiting for a decision and the ones
// already approved, and lets a paired phone approve, deny or revoke. Only a
// device key can do this; a computer-kind key is refused on every purpose
// but its own.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if s.outsideAgents == nil {
		http.Error(w, "outside agents unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-agents-admin:list:GET:/agents"); !ok {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		device, ok := s.authenticateRequest(w, r, "mewla-agents-admin:decide:POST:/agents:sha256="+hex.EncodeToString(sum[:]))
		if !ok {
			return
		}
		var decision agentsDecision
		if err := json.Unmarshal(body, &decision); err != nil || decision.ID == "" {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		var err2 error
		switch decision.Action {
		case "approve", "deny":
			_, err2 = s.outsideAgents.Decide(decision.ID, "", decision.Action == "approve", device.Name)
		case "revoke":
			err2 = s.outsideAgents.Revoke(decision.ID)
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		if err2 != nil {
			status := http.StatusBadRequest
			if errors.Is(err2, outsideagents.ErrNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err2.Error(), status)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pending := []pendingAgentView{}
	for _, p := range s.outsideAgents.PendingList() {
		pending = append(pending, pendingAgentView{ID: p.ID, Name: p.Name, Number: p.Number, ExpiresAt: p.ExpiresAt})
	}
	agents := []agentView{}
	for _, a := range s.outsideAgents.Agents() {
		agents = append(agents, agentView{ID: a.ID, Name: a.Name, ApprovedAt: a.ApprovedAt, LastUsedAt: a.LastUsedAt})
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "mewla-agents", map[string]any{"pending": pending, "agents": agents})
}
