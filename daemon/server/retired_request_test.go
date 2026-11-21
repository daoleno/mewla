package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// An app from before the Telegram channel was removed still asks for its
// status when Settings opens. The daemon answers with an error that carries the
// request id, so that app shows "Unavailable" and stops waiting.
func TestRetiredTelegramRequestIsAnsweredWithAnErrorForItsRequestID(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, nil, nil)
	upgrader := websocket.Upgrader{}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		defer attachTestOutbox(s, conn)()
		s.handleClientMessage(conn, []byte(`{"type":"telegram_connection_status","request_id":"tg-1"}`))
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
		Code      string `json:"code"`
	}
	if err := conn.ReadJSON(&answer); err != nil {
		t.Fatal(err)
	}
	if answer.Type != "error" || answer.RequestID != "tg-1" || answer.Code != "unknown_message_type" {
		t.Fatalf("answered %+v, want an unknown_message_type error for tg-1", answer)
	}
}
