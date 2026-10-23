package server

import "github.com/gorilla/websocket"

// DirectEndpoint is a LAN or Tailscale address a paired phone may try before
// the relay. It is only a hint: the phone reaches it through a TLS stream
// pinned to this computer's transport key and then checks a challenge signed
// with the daemon key, so a wrong or stale address can only fail.
type DirectEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Kind string `json:"kind"` // "lan" or "tailscale"
}

// SetDirectEndpoints supplies the addresses this computer is listening on
// right now. It is read on every connect, so a network change shows up when the
// phone next connects.
func (s *Server) SetDirectEndpoints(source func() []DirectEndpoint) {
	s.directEndpoints = source
}

// sendDirectEndpoints tells an authenticated phone where it can reach this
// computer directly. The list is replaced wholesale on each connect.
func (s *Server) sendDirectEndpoints(conn *websocket.Conn) {
	if s.directEndpoints == nil {
		return
	}
	endpoints := s.directEndpoints()
	if endpoints == nil {
		endpoints = []DirectEndpoint{}
	}
	s.sendJSON(conn, map[string]any{
		"type":      "direct_addresses",
		"addresses": endpoints,
	})
}
