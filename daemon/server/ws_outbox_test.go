package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

// A client that stops reading must not slow broadcast, the other clients or
// request handling, and it must be disconnected.
func TestStalledClientCostsOnlyItsOwnConnection(t *testing.T) {
	previousTimeout, previousLimit := wsWriteTimeout, wsOutboxLimit
	// The long write timeout shows nobody waits for it; the small queue limit
	// drops the stalled client.
	wsWriteTimeout, wsOutboxLimit = 10*time.Second, 1<<20
	defer func() { wsWriteTimeout, wsOutboxLimit = previousTimeout, previousLimit }()

	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	server.sendActionOverride = func(string, string) error { return nil }
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	socketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-connect"))
	stalled, _, err := stalledReaderDialer().Dial(socketURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	waitForClientCount(t, server, 1)
	healthy := dialDeviceWebSocket(t, socketURL, privateKey, manager.DaemonID(), deviceID)
	defer healthy.Close()
	waitForClientCount(t, server, 2)

	received := make(chan map[string]any, 256)
	go func() {
		defer close(received)
		for {
			var payload map[string]any
			if err := healthy.ReadJSON(&payload); err != nil {
				return
			}
			delete(payload, "pad")
			received <- payload
		}
	}()
	next := func(want string) map[string]any {
		t.Helper()
		timeout := time.After(2 * time.Second)
		for {
			select {
			case payload, ok := <-received:
				if !ok {
					t.Fatalf("healthy client disconnected while waiting for %s", want)
				}
				if payload["type"] == want {
					return payload
				}
			case <-timeout:
				t.Fatalf("healthy client did not receive %s", want)
			}
		}
	}

	pad := strings.Repeat("x", 128<<10)
	var slowest time.Duration
	for seq := 0; seq < 128; seq++ {
		started := time.Now()
		server.broadcastJSON(map[string]any{"type": "probe", "seq": seq, "pad": pad})
		if elapsed := time.Since(started); elapsed > slowest {
			slowest = elapsed
		}
		if got := next("probe"); got["seq"] != float64(seq) {
			t.Fatalf("probe order: got %v, want %d", got["seq"], seq)
		}
	}
	if slowest > 500*time.Millisecond {
		t.Fatalf("broadcast waited %v on a client that stopped reading", slowest)
	}
	waitForClientCount(t, server, 1)

	if err := healthy.WriteJSON(clientMessage{Type: "send_action", RequestID: "after-stall", WorkerID: "agent-one", Action: "continue"}); err != nil {
		t.Fatal(err)
	}
	next("action_sent")
}

// The writer gives up on a client that stops reading, without the producer
// ever touching the socket.
func TestOutboxDropsAClientThatStopsReading(t *testing.T) {
	previousTimeout, previousLimit := wsWriteTimeout, wsOutboxLimit
	wsWriteTimeout, wsOutboxLimit = 200*time.Millisecond, 64<<20
	defer func() { wsWriteTimeout, wsOutboxLimit = previousTimeout, previousLimit }()

	serverConn, _ := upgradedTestPair(t, stalledReaderDialer())
	outbox := newClientOutbox(serverConn)
	payload := []byte(strings.Repeat("x", 1<<20))
	started := time.Now()
	for i := 0; i < 32; i++ {
		if err := outbox.enqueue(websocket.TextMessage, payload, false); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("enqueue waited %v on the socket", elapsed)
	}
	deadline := time.Now().Add(5 * time.Second)
	for outbox.enqueue(websocket.TextMessage, []byte("x"), false) == nil {
		if time.Now().After(deadline) {
			t.Fatal("writer kept a client that stopped reading")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A slow link that keeps draining is not dropped, even when one message takes
// many write timeouts to deliver: the deadline measures progress.
func TestOutboxKeepsASlowButProgressingClient(t *testing.T) {
	previousTimeout, previousLimit := wsWriteTimeout, wsOutboxLimit
	wsWriteTimeout, wsOutboxLimit = 250*time.Millisecond, 64<<20
	defer func() { wsWriteTimeout, wsOutboxLimit = previousTimeout, previousLimit }()

	serverConn, client := upgradedTestPair(t, stalledReaderDialer())
	if tcp, ok := serverConn.NetConn().(*net.TCPConn); ok {
		_ = tcp.SetWriteBuffer(4096)
	}
	outbox := newClientOutbox(serverConn)
	defer outbox.close()

	// About 4 MB/s against a 250 ms deadline: the message takes several
	// deadlines, each 32 KiB chunk a few milliseconds.
	payload := []byte(strings.Repeat("s", 3<<20))
	if err := outbox.enqueue(websocket.BinaryMessage, payload, false); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, reader, err := client.NextReader()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16<<10)
	total := 0
	for {
		n, err := reader.Read(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("slow reader dropped after %d of %d bytes in %v: %v", total, len(payload), time.Since(started), err)
		}
		time.Sleep(4 * time.Millisecond)
	}
	if total != len(payload) {
		t.Fatalf("received %d bytes, want %d", total, len(payload))
	}
	if elapsed := time.Since(started); elapsed < 2*wsWriteTimeout {
		t.Fatalf("delivery took %v; the reader was not slow enough to exercise the deadline", elapsed)
	}
	if err := outbox.enqueue(websocket.TextMessage, []byte("after"), false); err != nil {
		t.Fatalf("slow client was dropped: %v", err)
	}
}

// A terminal flood on a slow but live client waits for room instead of
// filling the queue, so a broadcast to that client never overflows it.
func TestTerminalFloodDoesNotDropASlowReader(t *testing.T) {
	previousTimeout, previousLimit := wsWriteTimeout, wsOutboxLimit
	wsWriteTimeout, wsOutboxLimit = 5*time.Second, 1<<20
	defer func() { wsWriteTimeout, wsOutboxLimit = previousTimeout, previousLimit }()

	serverConn, client := upgradedTestPair(t, stalledReaderDialer())
	outbox := newClientOutbox(serverConn)
	defer outbox.close()

	counts := make(chan map[byte]int, 1)
	go func() {
		seen := map[byte]int{}
		defer func() { counts <- seen }()
		for seen['t']+seen['b'] < 256+32 {
			_, data, err := client.ReadMessage()
			if err != nil {
				return
			}
			seen[data[0]]++
			time.Sleep(time.Millisecond)
		}
	}()

	flood := make(chan error, 1)
	go func() {
		chunk := []byte(strings.Repeat("t", 64<<10))
		for i := 0; i < 256; i++ {
			if err := outbox.enqueue(websocket.TextMessage, chunk, true); err != nil {
				flood <- err
				return
			}
		}
		flood <- nil
	}()
	broadcast := []byte(strings.Repeat("b", 64<<10))
	for i := 0; i < 32; i++ {
		if err := outbox.enqueue(websocket.TextMessage, broadcast, false); err != nil {
			t.Fatalf("broadcast %d dropped a slow reader: %v", i, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := <-flood; err != nil {
		t.Fatalf("terminal flood: %v", err)
	}
	select {
	case seen := <-counts:
		if seen['t'] != 256 || seen['b'] != 32 {
			t.Fatalf("delivered terminal=%d broadcast=%d", seen['t'], seen['b'])
		}
	case <-time.After(20 * time.Second):
		t.Fatal("slow reader did not receive the flood")
	}
}

// attachTestOutbox registers conn for a handler-level test. The returned
// detach waits for queued messages to be written first.
func attachTestOutbox(s *Server, conn *websocket.Conn) func() {
	outbox := newClientOutbox(conn)
	s.mu.Lock()
	s.outboxes[conn] = outbox
	s.mu.Unlock()
	return func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			outbox.mu.Lock()
			idle := outbox.closed || outbox.bytes == 0
			outbox.mu.Unlock()
			if idle {
				break
			}
			time.Sleep(time.Millisecond)
		}
		s.mu.Lock()
		delete(s.outboxes, conn)
		s.mu.Unlock()
		outbox.close()
	}
}

// stalledReaderDialer gives the client a tiny receive buffer, so a client
// that never reads stalls the server's writes quickly.
func stalledReaderDialer() *websocket.Dialer {
	dialer := &net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var sockErr error
		if err := raw.Control(func(fd uintptr) {
			sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096)
		}); err != nil {
			return err
		}
		return sockErr
	}}
	return &websocket.Dialer{NetDialContext: dialer.DialContext, HandshakeTimeout: 5 * time.Second}
}

func upgradedTestPair(t *testing.T, dialer *websocket.Dialer) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(host.Close)
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), http.Header{"Origin": {host.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case conn := <-accepted:
		t.Cleanup(func() { _ = conn.Close() })
		return conn, client
	case <-time.After(2 * time.Second):
		t.Fatal("server side of the test socket never upgraded")
		return nil, nil
	}
}

func waitForClientCount(t *testing.T, server *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for server.clientCount() != want {
		if time.Now().After(deadline) {
			t.Fatalf("client count=%d, want %d", server.clientCount(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWireMessageTypeReadsOnlyTheTopLevelType(t *testing.T) {
	cases := []struct {
		name string
		kind int
		data string
		want string
	}{
		{"leading key", websocket.TextMessage, `{"type":"brain_snapshot","x":1}`, "brain_snapshot"},
		{"after nested objects, maps sort type late", websocket.TextMessage, `{"items":[{"type":"inner"}],"meta":{"type":"nested"},"type":"work_items_snapshot"}`, "work_items_snapshot"},
		{"type inside a string value is not a key", websocket.TextMessage, `{"text":"a \"type\":\"fake\" b","type":"real"}`, "real"},
		{"escaped quote and backslash before the key", websocket.TextMessage, `{"a":"x\\","b":"\"","type":"ok"}`, "ok"},
		{"whitespace after colon", websocket.TextMessage, ` { "type" : "spaced" }`, "spaced"},
		{"non-string type is skipped", websocket.TextMessage, `{"type":3,"other":1}`, "unknown"},
		{"no type", websocket.TextMessage, `{"a":1}`, "unknown"},
		{"not an object", websocket.TextMessage, `[{"type":"x"}]`, "unknown"},
		{"truncated", websocket.TextMessage, `{"a":"unterminated`, "unknown"},
		{"binary frame", websocket.BinaryMessage, `{"type":"terminal"}`, "binary"},
	}
	for _, tc := range cases {
		if got := wireMessageType(tc.kind, []byte(tc.data)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A dropped connection's log line must say what filled the link, with the
// types adding up to what was sent.
func TestOutboxTalliesBytesByMessageType(t *testing.T) {
	serverConn, client := upgradedTestPair(t, stalledReaderDialer())
	outbox := newClientOutbox(serverConn)
	defer outbox.close()

	big := `{"type":"brain_snapshot","pad":"` + strings.Repeat("p", 5000) + `"}`
	small := `{"type":"worker_session_updated","n":1}`
	for _, msg := range []string{big, small, small, `{"type":"direct_addresses"}`} {
		if err := outbox.enqueue(websocket.TextMessage, []byte(msg), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := outbox.enqueue(websocket.BinaryMessage, []byte("bytes"), false); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, _, err := client.ReadMessage(); err != nil {
			t.Fatal(err)
		}
	}
	var stats outboxStats
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if stats = outbox.stats(); len(stats.byType) == 4 {
			break
		}
	}
	if len(stats.byType) != 4 || stats.byType[0].kind != "brain_snapshot" || stats.byType[0].bytes != int64(len(big)) {
		t.Fatalf("heaviest type should lead: %+v", stats.byType)
	}
	var sum int64
	for _, stat := range stats.byType {
		sum += stat.bytes
	}
	if sum != stats.sent {
		t.Fatalf("types sum to %d bytes, sent=%d", sum, stats.sent)
	}
	line := (&wsLink{deflate: true}).describe(stats)
	for _, want := range []string{"deflate=true", "types=brain_snapshot:" + strconv.Itoa(len(big)) + "B/1", "worker_session_updated:" + strconv.Itoa(2*len(small)) + "B/2"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q lacks %q", line, want)
		}
	}
}
