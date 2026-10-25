package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/gorilla/websocket"
)

func readThinProxyResponse(t *testing.T, conn *websocket.Conn, within time.Duration) thinProxyResponse {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		t.Fatal(err)
	}
	var response thinProxyResponse
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("read WebSocket response: %v", err)
	}
	return response
}

// A send that waits on the provider must not stop the connection from reading:
// the read loop also answers subscribes, diagnostics and keepalive.
func TestSendInputWaitingOnTheProviderDoesNotBlockTheReadLoop(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	srv := &Server{
		sendInputOverride: func(string, string) error {
			entered <- struct{}{}
			<-release
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	if err := conn.WriteJSON(clientMessage{Type: "send_input", RequestID: "slow", WorkerID: "agent", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	<-entered
	// The provider call is parked. A request sent now is still read and
	// answered: a blocked read loop would never get to it.
	if err := conn.WriteJSON(clientMessage{Type: "send_action", RequestID: "stop", WorkerID: "agent", Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	stop := readThinProxyResponse(t, conn, 2*time.Second)
	if stop.RequestID != "stop" {
		t.Fatalf("a queued send held up the next request: %#v", stop)
	}
	close(release)
	sent := readThinProxyResponse(t, conn, 2*time.Second)
	if sent.Type != "input_sent" || sent.RequestID != "slow" {
		t.Fatalf("send response = %#v", sent)
	}
}

// Messages sent in a row reach the provider in the order they were read, even
// while an earlier one is still waiting on it.
func TestSendInputKeepsOrderPerWorker(t *testing.T) {
	var mu sync.Mutex
	var order []string
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	srv := &Server{
		sendInputOverride: func(workerID, text string) error {
			if text == "one\n" {
				close(firstEntered)
				<-releaseFirst
			}
			mu.Lock()
			order = append(order, workerID+":"+text)
			mu.Unlock()
			return nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	for i, text := range []string{"one", "two", "three"} {
		if err := conn.WriteJSON(clientMessage{Type: "send_input", RequestID: fmt.Sprintf("r%d", i), WorkerID: "agent", Text: text}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			<-firstEntered
		}
	}
	// A different worker is not held behind this lane.
	if err := conn.WriteJSON(clientMessage{Type: "send_input", RequestID: "other", WorkerID: "other-agent", Text: "parallel"}); err != nil {
		t.Fatal(err)
	}
	other := readThinProxyResponse(t, conn, 2*time.Second)
	if other.RequestID != "other" || other.Type != "input_sent" {
		t.Fatalf("another worker waited on a busy lane: %#v", other)
	}
	close(releaseFirst)
	var got []string
	for range 3 {
		got = append(got, readThinProxyResponse(t, conn, 2*time.Second).RequestID)
	}
	if fmt.Sprint(got) != "[r0 r1 r2]" {
		t.Fatalf("acknowledgements out of order: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(order) != "[other-agent:parallel\n agent:one\n agent:two\n agent:three\n]" {
		t.Fatalf("provider order = %v", order)
	}
}

// The ack of a send can be lost with its connection while the daemon is still
// handling it. The phone resends the same request id on its new connection;
// both must end in exactly one provider turn, and the resend gets its ack.
func TestSendInputResentOnANewConnectionWhileTheFirstIsInFlightDeliversOnce(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-host:@resend-in-flight"
	threadID := "thread-resend-in-flight"
	requestID := "request-resend-in-flight"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := &Server{
		brain: brain.NewService(store, nil, nil),
		sendInputWithReceiptOverride: func(_, _, _ string) error {
			providerCalls.Add(1)
			entered <- struct{}{}
			<-release
			return nil
		},
	}
	request := clientMessage{
		Type: "send_input", RequestID: requestID, WorkerID: hostID, Text: "once only",
		ConversationScopeKey: "brain-thread:" + threadID,
	}

	first := openThinProxyTestSocket(t, srv)
	if err := first.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	<-entered
	// The first connection dies mid-send; its handler keeps running.
	_ = first.Close()
	second := openThinProxyTestSocket(t, srv)
	if err := second.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	// The resend waits its turn on the worker's lane instead of racing.
	select {
	case <-entered:
		t.Fatal("the resend reached the provider while the first send was in flight")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	response := readThinProxyResponse(t, second, 3*time.Second)
	if response.Type != "input_sent" || response.RequestID != requestID {
		t.Fatalf("resend response = %#v", response)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want exactly 1", got)
	}
}

// A client that stops answering is dropped once it is silent past the idle
// timeout; one that keeps answering pings stays.
func TestWebSocketKeepaliveDropsASilentClientAndKeepsAnAnsweringOne(t *testing.T) {
	oldPing, oldIdle := wsPingInterval, wsReadIdleTimeout
	wsPingInterval, wsReadIdleTimeout = 40*time.Millisecond, 250*time.Millisecond
	t.Cleanup(func() { wsPingInterval, wsReadIdleTimeout = oldPing, oldIdle })

	serverSide := make(chan error, 2)
	run := func(readOnClient bool) {
		pair := newKeepalivePair(t, serverSide)
		if readOnClient {
			// Reading is what lets the client library answer pings.
			go func() {
				for {
					if _, _, err := pair.client.ReadMessage(); err != nil {
						return
					}
				}
			}()
		}
	}
	run(false)
	select {
	case err := <-serverSide:
		if err == nil {
			t.Fatal("silent client ended without an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a silent client was never dropped")
	}

	run(true)
	select {
	case err := <-serverSide:
		t.Fatalf("a client answering pings was dropped: %v", err)
	case <-time.After(900 * time.Millisecond):
	}
}

type keepalivePair struct {
	client *websocket.Conn
}

// newKeepalivePair serves one connection under keepWebSocketAlive and reports
// the error that ends its read loop.
func newKeepalivePair(t *testing.T, ended chan<- error) keepalivePair {
	t.Helper()
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		link := keepWebSocketAlive(conn)
		defer link.stop()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				ended <- err
				return
			}
		}
	}))
	t.Cleanup(httpServer.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return keepalivePair{client: client}
}
