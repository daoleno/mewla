package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/daoleno/mewla/daemon/grants"
	"github.com/daoleno/mewla/daemon/work"
)

// GrantRequests answers the buttons of a permission card in Brain's thread
// (grants.Manager).
type GrantRequests interface {
	Act(ctx context.Context, id, action string) (work.CodexConversationEvent, error)
}

// SetGrantRequests installs the permission card's owner.
func (s *Server) SetGrantRequests(requests GrantRequests) { s.grantRequests = requests }

// grantRequestAction is one tap on a card: open_settings, not_now or restart.
type grantRequestAction struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// handleGrantRequest is POST /brain/grant-request {id, action}: the card as
// the conversation feed carries it after the action, or {"error"} with 404
// (no such card), 409 (the card is not where the action needs it) or 400.
// The signature binds the body.
func (s *Server) handleGrantRequest(w http.ResponseWriter, r *http.Request) {
	if s.grantRequests == nil {
		http.Error(w, "permission cards unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	sum := sha256.Sum256(body)
	if _, ok := s.authenticateRequest(w, r, "mewla-grant-request:POST:/brain/grant-request:sha256="+hex.EncodeToString(sum[:])); !ok {
		return
	}
	var action grantRequestAction
	if err := json.Unmarshal(body, &action); err != nil || action.ID == "" || action.Action == "" {
		writeGrantRequestJSON(w, http.StatusBadRequest, map[string]string{"error": "send {id, action}"})
		return
	}
	event, err := s.grantRequests.Act(r.Context(), action.ID, action.Action)
	if err != nil {
		var conflict *grants.ConflictError
		switch {
		case errors.Is(err, grants.ErrNotFound):
			writeGrantRequestJSON(w, http.StatusNotFound, map[string]string{"error": "This request is gone."})
		case errors.As(err, &conflict):
			writeGrantRequestJSON(w, http.StatusConflict, map[string]string{"error": conflict.Message})
		case errors.Is(err, grants.ErrUnknownAction):
			writeGrantRequestJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeGrantRequestJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeGrantRequestJSON(w, http.StatusOK, event)
}

func writeGrantRequestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
