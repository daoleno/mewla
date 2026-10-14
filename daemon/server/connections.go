package server

import (
	"context"
	"strings"

	"github.com/daoleno/mewla/daemon/connections"
	"github.com/gorilla/websocket"
)

func (s *Server) SetConnections(manager *connections.Manager) {
	s.connections = manager
	if manager != nil {
		manager.SetWebOrigins(s.servesWebOrigin)
	}
}
func (s *Server) handleConnections(conn *websocket.Conn, raw clientMessage) {
	if raw.ConnectionRequest == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_request", "Plugin request is required")
		return
	}
	// cf runs for seconds; it must not hold up this client's other messages.
	if strings.HasPrefix(raw.ConnectionRequest.Action, "cloudflare_") {
		go s.answerConnections(conn, raw)
		return
	}
	s.answerConnections(conn, raw)
}
func (s *Server) answerConnections(conn *websocket.Conn, raw clientMessage) {
	result, err := s.connections.Handle(context.Background(), *raw.ConnectionRequest)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "plugin_request_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{"type": "connections_result", "request_id": raw.RequestID, "connections": result})
}
