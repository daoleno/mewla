package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/daoleno/mewla/daemon/agentuse"
)

// iphoneAction is one change from the app. Exactly one field is set.
type iphoneAction struct {
	// Enabled is the iPhone use switch (Settings → Labs).
	Enabled *bool `json:"enabled,omitempty"`
	// StopAgent ends the agent using the iPhone.
	StopAgent bool `json:"stop_agent,omitempty"`
	// HandoffDone answers an agent's handoff: the person did what it asked.
	HandoffDone bool `json:"handoff_done,omitempty"`
}

// handleIPhone reports iPhone use (GET), or changes its switch, stops the
// agent or answers a handoff (POST). A change binds its signature to the
// body, like /phone.
func (s *Server) handleIPhone(w http.ResponseWriter, r *http.Request) {
	if s.agentUse == nil {
		http.Error(w, "iPhone use unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-iphone:GET:/iphone"); !ok {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-iphone:POST:/iphone:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var action iphoneAction
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&action); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		set := 0
		for _, on := range []bool{action.Enabled != nil, action.StopAgent, action.HandoffDone} {
			if on {
				set++
			}
		}
		if set != 1 {
			http.Error(w, "send one of enabled, stop_agent or handoff_done", http.StatusBadRequest)
			return
		}
		switch {
		case action.Enabled != nil:
			if _, err := s.agentUse.SetIPhoneEnabled(*action.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case action.StopAgent:
			// Stopping an agent is always safe, so any paired device may.
			if err := s.agentUse.StopIPhone(); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		case action.HandoffDone:
			s.agentUse.IPhoneHandoffDone()
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row := s.agentUse.IPhone()
	if row == nil {
		row = &agentuse.IPhoneStatus{State: agentuse.IPhoneUnsupported, Summary: "iPhone use works through iPhone Mirroring on a Mac. Switch to the Mewla on your Mac."}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(row)
}
