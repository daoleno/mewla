package server

import (
	"context"
	"github.com/daoleno/zen/daemon/connections"
	"github.com/gorilla/websocket"
)

func (s *Server) SetConnections(manager *connections.Manager) { s.connections = manager }
func (s *Server) handleConnections(conn *websocket.Conn, raw clientMessage) {
	if raw.ConnectionRequest == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_request", "Plugin request is required")
		return
	}
	result, err := s.connections.Handle(context.Background(), *raw.ConnectionRequest)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "plugin_request_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{"type": "connections_result", "request_id": raw.RequestID, "connections": result})
}
