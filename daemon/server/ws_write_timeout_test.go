package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A client that never reads must not hold its write lock forever: the write
// times out, the connection closes, and later writes fail fast.
func TestWriteMessageGivesUpOnAClientThatStopsReading(t *testing.T) {
	previous := wsWriteTimeout
	wsWriteTimeout = 200 * time.Millisecond
	defer func() { wsWriteTimeout = previous }()

	s := &Server{writes: map[*websocket.Conn]*sync.Mutex{}}
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		s.mu.Lock()
		s.writes[conn] = &sync.Mutex{}
		s.mu.Unlock()
		payload := []byte(strings.Repeat("x", 1<<20))
		for i := 0; i < 256; i++ {
			if err := s.writeMessage(conn, websocket.TextMessage, payload); err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}))
	defer server.Close()

	header := http.Header{"Origin": {server.URL}}
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// The client never reads, so the socket buffers fill and a write stalls.

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("256 MiB was written to a client that never reads")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("writeMessage blocked on a client that stopped reading")
	}
}
