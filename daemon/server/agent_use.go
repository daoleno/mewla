package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/daoleno/mewla/daemon/agentuse"
)

// SetAgentUse installs this computer's "agents can use this computer" switch
// and status.
func (s *Server) SetAgentUse(service *agentuse.Service) { s.agentUse = service }

// agentUseAction is one change from the app. Exactly one field is set.
type agentUseAction struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Fix     string  `json:"fix,omitempty"`
	Key     *string `json:"key,omitempty"`
	// StopAgent ends the agent using the screen.
	StopAgent bool `json:"stop_agent,omitempty"`
	// Follow is a grant the person is giving at this computer now (the Mac
	// app's permission panel): Mewla applies it once given.
	Follow string `json:"follow,omitempty"`
	// Restart starts Mewla again so a given grant takes effect.
	Restart bool `json:"restart,omitempty"`
}

// fields counts the action's set fields: exactly one is allowed.
func (a agentUseAction) fields() int {
	n := 0
	for _, set := range []bool{a.Enabled != nil, a.Fix != "", a.Key != nil, a.StopAgent, a.Follow != "", a.Restart} {
		if set {
			n++
		}
	}
	return n
}

// handleAgentUse reports what agents can use on this computer and applies
// the app's one switch and fixes. A change binds its signature to the body:
// it may carry the extension's key.
func (s *Server) handleAgentUse(w http.ResponseWriter, r *http.Request) {
	if s.agentUse == nil {
		http.Error(w, "agent use unavailable", http.StatusServiceUnavailable)
		return
	}
	var status agentuse.Status
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-agent-use:GET:/agent-use"); !ok {
			return
		}
		status = s.agentUse.Status()
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-agent-use:POST:/agent-use:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var action agentUseAction
		if err := json.Unmarshal(body, &action); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		switch {
		case action.fields() != 1:
			http.Error(w, "send one of enabled, fix, key, stop_agent, follow or restart", http.StatusBadRequest)
			return
		case action.StopAgent:
			status, err = s.agentUse.StopAgent()
		case action.Enabled != nil:
			status, err = s.agentUse.SetEnabled(*action.Enabled)
		case action.Fix != "":
			status, err = s.agentUse.Fix(action.Fix)
		case action.Key != nil:
			status, err = s.agentUse.SetKey(*action.Key)
		case action.Follow != "":
			status, err = s.agentUse.FollowPermission(action.Follow)
		default:
			status, err = s.agentUse.RestartForPermission()
		}
		if errors.Is(err, agentuse.ErrNothingToRestart) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(status)
}
