package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The app asks with link_ping right after its network changed and replaces a
// socket that does not answer in a few seconds, so the answer is immediate and
// carries the request id.
func TestLinkPingIsAnsweredWithItsRequestID(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, nil, nil)
	upgrader := websocket.Upgrader{}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		defer attachTestOutbox(s, conn)()
		s.handleClientMessage(conn, []byte(`{"type":"link_ping","request_id":"ping-1"}`))
	}))
	defer host.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var answer struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := conn.ReadJSON(&answer); err != nil {
		t.Fatal(err)
	}
	if answer.Type != "link_pong" || answer.RequestID != "ping-1" {
		t.Fatalf("link_ping answered %+v, want link_pong for ping-1", answer)
	}
}
