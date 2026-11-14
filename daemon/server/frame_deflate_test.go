package server

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A client whose WebSocket cannot negotiate permessage-deflate (NSH's native
// one) offers frameDeflateSubprotocol beside its auth subprotocol. The daemon
// then sends every JSON message of frameDeflateMin bytes or more as one binary
// message of raw DEFLATE, which inflates to exactly the text it would have
// sent; smaller messages stay text. The handshake still selects the auth
// subprotocol, which a browser requires.
func TestFrameDeflateSendsLargeJSONAsRawDeflate(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	auth := authSubprotocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-connect")))

	dialer := websocket.Dialer{Subprotocols: []string{auth, frameDeflateSubprotocol}}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if got := conn.Subprotocol(); got != auth {
		t.Fatalf("selected subprotocol = %q, want the auth one", got)
	}
	waitForClientCount(t, server, 1)

	big := map[string]any{"type": "brain_snapshot", "pad": strings.Repeat("a long transcript line\n", 2000)}
	small := map[string]any{"type": "worker_session_heartbeat"}
	server.mu.Lock()
	var serverConn *websocket.Conn
	for c := range server.clients {
		serverConn = c
	}
	server.mu.Unlock()
	server.sendJSON(serverConn, big)
	server.sendJSON(serverConn, small)

	wantBig, _ := json.Marshal(big)
	wantSmall, _ := json.Marshal(small)
	sawBig, sawSmall := false, false
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for !sawBig || !sawSmall {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (big %v, small %v)", err, sawBig, sawSmall)
		}
		text := data
		if kind == websocket.BinaryMessage {
			text, err = io.ReadAll(flate.NewReader(bytes.NewReader(data)))
			if err != nil {
				t.Fatalf("a binary message is not raw DEFLATE: %v", err)
			}
		}
		var frame map[string]any
		if err := json.Unmarshal(text, &frame); err != nil {
			t.Fatalf("message %d is not JSON: %v", kind, err)
		}
		if len(text) >= frameDeflateMin && kind != websocket.BinaryMessage {
			t.Fatalf("a %d-byte %v went out as text", len(text), frame["type"])
		}
		switch frame["type"] {
		case "brain_snapshot":
			if _, padded := frame["pad"]; !padded {
				continue // the snapshot every connection gets first
			}
			if !bytes.Equal(text, wantBig) {
				t.Fatal("the inflated message differs from the JSON the daemon sent")
			}
			if len(data)*10 > len(text) {
				t.Fatalf("deflated %d bytes to %d", len(text), len(data))
			}
			sawBig = true
		case "worker_session_heartbeat":
			if kind != websocket.TextMessage || !bytes.Equal(text, wantSmall) {
				t.Fatalf("a small message changed: kind %d %s", kind, text)
			}
			sawSmall = true
		}
	}
	stats := server.outboxStats(serverConn)
	if !stats.frameDeflate || stats.wire >= stats.sent {
		t.Fatalf("stats: frameDeflate %v wire %d sent %d", stats.frameDeflate, stats.wire, stats.sent)
	}
	if line := (&wsLink{}).describe(stats); !strings.Contains(line, " frames=deflate-raw wire=") {
		t.Fatalf("the connection line does not say the frames were deflated: %s", line)
	}
}

// A client that does not offer it (a browser, which has permessage-deflate,
// or an app from before) gets every message as text.
func TestFrameDeflateNeedsTheOffer(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn := dialDeviceWebSocket(t, wsURL, privateKey, manager.DaemonID(), deviceID)
	defer conn.Close()
	waitForClientCount(t, server, 1)
	server.mu.Lock()
	var serverConn *websocket.Conn
	for c := range server.clients {
		serverConn = c
	}
	server.mu.Unlock()
	server.sendJSON(serverConn, map[string]any{"type": "brain_snapshot", "pad": strings.Repeat("x", 4*frameDeflateMin)})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if kind != websocket.TextMessage {
			t.Fatalf("a client that did not offer %s got a binary message", frameDeflateSubprotocol)
		}
		if bytes.Contains(data, []byte(`"pad"`)) {
			break
		}
	}
	if line := (&wsLink{}).describe(server.outboxStats(serverConn)); strings.Contains(line, "frames=") {
		t.Fatalf("connection line: %s", line)
	}
}
