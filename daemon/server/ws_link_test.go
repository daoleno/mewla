package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

// serveLink serves one connection under keepWebSocketAlive and returns the
// link with the error that ended its read loop.
func serveLink(t *testing.T) (client *websocket.Conn, link func() *wsLink, ended <-chan error) {
	t.Helper()
	linkCh := make(chan *wsLink, 1)
	endedCh := make(chan error, 1)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		l := keepWebSocketAlive(conn)
		defer l.stop()
		linkCh <- l
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				endedCh <- err
				return
			}
		}
	}))
	t.Cleanup(httpServer.Close)
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var l *wsLink
	select {
	case l = <-linkCh:
	case <-time.After(2 * time.Second):
		t.Fatal("connection never reached the handler")
	}
	return c, func() *wsLink { return l }, endedCh
}

func waitEnded(t *testing.T, ended <-chan error) error {
	t.Helper()
	select {
	case err := <-ended:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("the read loop never ended")
		return nil
	}
}

// The log must say which end hung up, because a phone's report ("error") is the
// same whichever did.
func TestWebSocketLinkNamesWhichEndHungUp(t *testing.T) {
	oldPing, oldIdle := wsPingInterval, wsReadIdleTimeout
	wsPingInterval, wsReadIdleTimeout = 40*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { wsPingInterval, wsReadIdleTimeout = oldPing, oldIdle })

	t.Run("the phone's side vanished", func(t *testing.T) {
		client, link, ended := serveLink(t)
		_ = client.UnderlyingConn().Close() // no close frame: a dropped connection
		err := waitEnded(t, ended)
		if cause := link().endCause(err, outboxStats{}); cause != "peer-closed" {
			t.Fatalf("cause = %q (%v)", cause, err)
		}
	})

	t.Run("the phone stopped answering", func(t *testing.T) {
		_, link, ended := serveLink(t) // never reads, so never answers a ping
		err := waitEnded(t, ended)
		if cause := link().endCause(err, outboxStats{}); cause != "client-silent" {
			t.Fatalf("cause = %q (%v)", cause, err)
		}
	})

	t.Run("the daemon closed it", func(t *testing.T) {
		_, link, ended := serveLink(t)
		link().closeBecause("ping not sent: websocket: write timeout")
		err := waitEnded(t, ended)
		if cause := link().endCause(err, outboxStats{}); cause != "daemon-closed(ping not sent: websocket: write timeout)" {
			t.Fatalf("cause = %q (%v)", cause, err)
		}
	})

	t.Run("the writer gave up on a client that could not keep up", func(t *testing.T) {
		_, link, ended := serveLink(t)
		_ = link().conn.Close()
		err := waitEnded(t, ended)
		out := outboxStats{failure: errOutboxOverflow.Error()}
		if cause := link().endCause(err, out); !strings.HasPrefix(cause, "daemon-closed(outbox: client fell too far behind") {
			t.Fatalf("cause = %q", cause)
		}
	})
}

// A phone that vanishes mid-session leaves one line with the cause and how the
// last send and the last ping went.
func TestLostClientLogLineCarriesCauseAndLinkState(t *testing.T) {
	buf := captureLog(t)
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	client := dialDeviceWebSocket(t, socketURL, privateKey, manager.DaemonID(), deviceID)
	waitForClientCount(t, server, 1)
	_ = client.UnderlyingConn().Close()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(buf.String(), "client connection lost") {
		if time.Now().After(deadline) {
			t.Fatalf("no line about the lost client; log: %q", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	line := buf.String()
	for _, want := range []string{"device=" + deviceID, "cause=peer-closed", "sent=", "pending=", "lastSend=", "pongs=0/0", "lastPong=never"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "Device ") {
		t.Errorf("the log holds an Authorization header: %q", line)
	}
}
