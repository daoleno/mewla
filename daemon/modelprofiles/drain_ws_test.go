package modelprofiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A Codex response in progress on a WebSocket finishes before the daemon
// stops; the connection then closes at that turn boundary (Codex opens a new
// one for its next response), and an idle connection closes at once.
func TestDrainEndsWebSocketsAtTurnBoundaries(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(wsEventCreated))
			<-release
			_ = conn.WriteMessage(websocket.TextMessage, []byte(wsEventCompleted))
		}
	}))
	defer upstream.Close()
	g := gatewayTest(t)
	g.SetUpstream(gatewayUpstreamFor(upstream.URL))
	headers := map[string]string{"OpenAI-Beta": "responses_websockets=2026-02-06"}
	busy, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", headers)
	idle, status2 := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", headers)
	if status != 101 || status2 != 101 {
		t.Fatalf("handshakes %d %d", status, status2)
	}
	if err := busy.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, got, err := busy.ReadMessage(); err != nil || string(got) != wsEventCreated {
		t.Fatalf("created: %s %v", got, err)
	}
	if g.InFlight() != 1 {
		t.Fatalf("in flight = %d, want the response in progress", g.InFlight())
	}

	drained := make(chan DrainResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		drained <- g.Drain(ctx)
	}()
	_ = idle.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := idle.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("idle connection: %v, want a going-away close", err)
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case result := <-drained:
		t.Fatalf("drain ended mid-response: %+v", result)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	_ = busy.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, got, err := busy.ReadMessage(); err != nil || string(got) != wsEventCompleted {
		t.Fatalf("the response in progress was cut: %s %v", got, err)
	}
	if _, _, err := busy.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseGoingAway) || !strings.Contains(err.Error(), wsDrainReason) {
		t.Fatalf("after its response: %v, want a going-away close", err)
	}
	select {
	case result := <-drained:
		if result.Cut != 0 || result.Waited != 1 {
			t.Fatalf("drain result %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not end after the last response")
	}
}
