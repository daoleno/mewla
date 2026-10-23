package server

import (
	"context"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

// ErrCodexStatusUnavailable says the Worker has no app-server to ask for its
// status: it is not a Codex that Mewla started.
var ErrCodexStatusUnavailable = errors.New("codex status unavailable")

// SetCodexStatus installs where a Codex Worker's status is read from.
func (s *Server) SetCodexStatus(read func(ctx context.Context, workerID string) (any, error)) {
	s.mu.Lock()
	s.codexStatus = read
	s.mu.Unlock()
}

func (s *Server) handleCodexStatus(conn *websocket.Conn, raw clientMessage) {
	s.mu.Lock()
	read := s.codexStatus
	s.mu.Unlock()
	if read == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_status_unavailable", "This Codex has no app-server to ask.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := read(ctx, raw.TargetID)
	if errors.Is(err, ErrCodexStatusUnavailable) {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_status_unavailable", "This Codex was not started by Mewla, so it has no app-server to ask.")
		return
	}
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_status_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "codex_status",
		"request_id": raw.RequestID,
		"target_id":  raw.TargetID,
		"status":     status,
	})
}
