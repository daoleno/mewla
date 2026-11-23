package server

import (
	"time"

	"github.com/daoleno/mewla/daemon/link"
	"github.com/gorilla/websocket"
)

// LinkRoute is this computer's Mewla Relay route: its id, the SHA-256 of its
// transport key, and the stable address on each relay.
type LinkRoute struct {
	RouteID      string
	TransportPin string
	Candidates   []link.PairingCandidate
}

// SetLinkRoute supplies the route, read on every connect; ok is false while
// this computer is on no relay.
func (s *Server) SetLinkRoute(source func() (LinkRoute, bool)) {
	s.linkRoute = source
}

// sendLinkRoute tells an authenticated app this computer's route, signed with
// the daemon key the app pinned at pairing. An app paired by the hosted https
// address takes its pairing as a Mewla Relay route from then on (its LAN and
// Tailscale addresses come next, in direct_addresses), with no new pairing.
// Older apps ignore the message.
func (s *Server) sendLinkRoute(conn *websocket.Conn) {
	if s.linkRoute == nil {
		return
	}
	route, ok := s.linkRoute()
	if !ok || route.RouteID == "" || len(route.TransportPin) != 64 || len(route.Candidates) == 0 {
		return
	}
	issued := time.Now().UnixMilli()
	candidates := make([]map[string]string, 0, len(route.Candidates))
	for _, candidate := range route.Candidates {
		candidates = append(candidates, map[string]string{"name": candidate.Name, "url": candidate.StableURL})
	}
	payload, err := link.RouteBindingPayload(s.auth.DaemonID(), s.auth.PublicKeyHex(), route.RouteID, route.TransportPin, issued, route.Candidates)
	if err != nil {
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":          "link_route",
		"version":       link.RouteVersion,
		"route_id":      route.RouteID,
		"transport_pin": route.TransportPin,
		"candidates":    candidates,
		"issued_at_ms":  issued,
		"signature":     s.auth.CreateLinkRouteSignature(payload),
	})
}
