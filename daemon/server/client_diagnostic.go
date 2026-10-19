package server

import (
	"log"
	"strings"

	"github.com/gorilla/websocket"
)

const maxClientDiagnosticDetail = 300

// handleClientDiagnostic writes one line a paired app reports about its own
// state (a stream gap, a snapshot that never arrived, a send that timed out).
// The app is the only place those moments are visible, and without a line here
// a stuck screen leaves nothing to look at afterwards.
func (s *Server) handleClientDiagnostic(conn *websocket.Conn, raw clientMessage) {
	s.mu.Lock()
	owner := s.clients[conn]
	s.mu.Unlock()
	deviceID := ""
	if owner != nil {
		deviceID = owner.deviceID
	}
	log.Print(clientDiagnosticLine(deviceID, raw))
}

func clientDiagnosticLine(deviceID string, raw clientMessage) string {
	return "client diagnostic device=" + deviceID +
		" event=" + sanitizeDiagnosticToken(raw.Name) +
		" stream=" + sanitizeDiagnosticToken(raw.RequestID) +
		" detail=" + quoteDiagnosticDetail(raw.Body)
}

func sanitizeDiagnosticToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 80 {
		value = value[:80]
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r < 0x21 || r == 0x7f {
			return '_'
		}
		return r
	}, value)
}

func quoteDiagnosticDetail(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxClientDiagnosticDetail {
		value = value[:maxClientDiagnosticDetail]
	}
	return strings.ReplaceAll(strings.ReplaceAll(`"`+strings.ReplaceAll(value, `"`, `'`)+`"`, "\n", " "), "\r", " ")
}
