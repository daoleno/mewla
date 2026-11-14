package server

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

// taillessSubprotocol is the WebSocket subprotocol a client offers, beside its
// auth subprotocol, to say it never shows a Session's terminal tail
// (last_output_lines): the NSH apps since v0.4.3. Such a client is sent the
// Session list and changes without it. A running Session's tail is most of
// each worker_session_updated (some 8-16 KB of mostly incompressible terminal
// text), which every connected app took every 10 s per Session, on a link that
// carries 60 KB/s. The handshake still selects the auth subprotocol; a client
// that does not offer this one gets the tail as before (the Expo app's
// terminal preview).
const taillessSubprotocol = "mewla.sessions.tailless"

func clientOffersTailless(r *http.Request) bool {
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == taillessSubprotocol {
			return true
		}
	}
	return false
}

// withoutTerminalTail is a worker_session_* message or list without each
// Session's last_output_lines, or nil when it has none to drop.
func withoutTerminalTail(data []byte) []byte {
	var message map[string]json.RawMessage
	if json.Unmarshal(data, &message) != nil {
		return nil
	}
	changed := false
	strip := func(raw json.RawMessage) json.RawMessage {
		var session map[string]json.RawMessage
		if json.Unmarshal(raw, &session) != nil {
			return raw
		}
		if _, ok := session["last_output_lines"]; !ok {
			return raw
		}
		delete(session, "last_output_lines")
		out, err := json.Marshal(session)
		if err != nil {
			return raw
		}
		changed = true
		return out
	}
	if raw, ok := message["worker_session"]; ok {
		message["worker_session"] = strip(raw)
	}
	if raw, ok := message["worker_sessions"]; ok {
		var sessions []json.RawMessage
		if json.Unmarshal(raw, &sessions) == nil {
			for i := range sessions {
				sessions[i] = strip(sessions[i])
			}
			if out, err := json.Marshal(sessions); err == nil {
				message["worker_sessions"] = out
			}
		}
	}
	if !changed {
		return nil
	}
	out, err := json.Marshal(message)
	if err != nil {
		return nil
	}
	return out
}

// broadcastFeed sends one Session feed message to every client: the tailless
// form to a client that asked for it (made once, whatever the client count).
func (s *Server) broadcastFeed(data []byte) {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.clients))
	tailless := false
	for conn := range s.clients {
		conns = append(conns, conn)
		if outbox := s.outboxes[conn]; outbox != nil && outbox.isTailless() {
			tailless = true
		}
	}
	s.mu.Unlock()
	var lite []byte
	if tailless {
		lite = withoutTerminalTail(data)
	}
	for _, conn := range conns {
		if err := s.queueFeedMessage(conn, data, lite); err != nil {
			s.mu.Lock()
			work := s.removeClientLocked(conn)
			s.mu.Unlock()
			s.completeClientDetach(work)
		}
	}
}

// queueFeedMessage queues a feed message for one client, tailless when it
// asked for that and lite is not nil.
func (s *Server) queueFeedMessage(conn *websocket.Conn, data, lite []byte) error {
	s.mu.Lock()
	outbox, ok := s.outboxes[conn]
	s.mu.Unlock()
	if !ok {
		return errOutboxClosed
	}
	if lite != nil && outbox.isTailless() {
		data = lite
	}
	return outbox.enqueue(websocket.TextMessage, data, false)
}
