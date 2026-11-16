package relay

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/linkproto"
)

// slotWatchListener hands out connections that note, at the relay's first
// write, how many connector handshakes the relay still counts as running.
type slotWatchListener struct {
	net.Listener
	server *Server
	mu     sync.Mutex
	held   []int
}

func (listener *slotWatchListener) Accept() (net.Conn, error) {
	conn, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &slotWatchConn{Conn: conn, listener: listener}, nil
}

type slotWatchConn struct {
	net.Conn
	listener *slotWatchListener
	once     sync.Once
}

func (conn *slotWatchConn) Write(data []byte) (int, error) {
	conn.once.Do(func() {
		server := conn.listener.server
		server.ipMu.Lock()
		perIP := 0
		for _, usage := range server.ipUsage {
			perIP += int(usage[ipConnectorHandshakes])
		}
		server.ipMu.Unlock()
		conn.listener.mu.Lock()
		conn.listener.held = append(conn.listener.held, len(server.connectorHandshakes)+perIP)
		conn.listener.mu.Unlock()
	})
	return conn.Conn.Write(data)
}

// A connector that has read why the relay refused it may connect again at
// once. If its finished handshake still held the slot, the relay's accept loop
// closed the new connection unread and the kernel's RST discarded the answer
// (CI 11-16: "read protocol rejection: connection reset by peer"). Every
// rejection is written after the slot is free, so the next dial always finds it.
func TestConnectorRejectionFreesItsHandshakeSlotBeforeAnswering(t *testing.T) {
	server := limitedRelay(t)
	server.config.MaxConnectorHandshakesPerIP = 1
	server.connectorHandshakes = make(chan struct{}, 1)
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	connectors := &slotWatchListener{Listener: inner, server: server}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx, clientListener, connectors) }()
	defer func() {
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("relay did not stop")
		}
	}()

	// With one slot, each refused connector dials again right after reading
	// its answer; every one of them must be answered, none reset.
	for i := 0; i < 20; i++ {
		sendConnectorReject(t, inner.Addr().String(), linkproto.Message{Type: "unsupported"})
	}
	connectors.mu.Lock()
	defer connectors.mu.Unlock()
	if len(connectors.held) != 20 {
		t.Fatalf("answers written=%d, want 20", len(connectors.held))
	}
	for i, held := range connectors.held {
		if held != 0 {
			t.Fatalf("rejection %d was written while %d handshake slot(s) were still held", i, held)
		}
	}
	if got := server.capacityRejects["connector_handshakes"].Load() + server.capacityRejects[ipLimitReasons[ipConnectorHandshakes]].Load(); got != 0 {
		t.Fatalf("capacity rejections=%d, want 0", got)
	}
}
