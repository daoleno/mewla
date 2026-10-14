package link

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestStreamGateClosesUnprovenStreamsAndEvictsTheOldest(t *testing.T) {
	gate := newStreamGate(2, 50*time.Millisecond)
	local, remote := net.Pipe()
	defer remote.Close()
	gate.admit(local)
	oldest := &closeCounter{}
	gate = newStreamGate(2, time.Hour)
	gate.admit(oldest)
	second := gate.admit(&closeCounter{})
	gate.admit(&closeCounter{})
	if oldest.closed != 1 {
		t.Fatal("a third unproven stream did not evict the oldest")
	}
	second.prove()
	gate.admit(&closeCounter{})
	if len(gate.unproven) != 2 {
		t.Fatalf("unproven = %d, want 2 after a proven stream left", len(gate.unproven))
	}
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); err == nil {
		t.Fatal("unproven stream outlived its lifetime")
	}
}

type closeCounter struct {
	net.Conn
	closed int
}

func (conn *closeCounter) Close() error { conn.closed++; return nil }

func TestStreamGateProvesOnlyServerVerifiedRequests(t *testing.T) {
	for _, verified := range []bool{false, true} {
		gate := newStreamGate(1, time.Hour)
		stream := gate.admit(&nopConn{})
		handler := stream.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A junk Authorization header on an open endpoint gets a 200
			// without the server verifying anything.
			if verified {
				auth.NotifyAuthenticated(r.Context())
			}
			w.WriteHeader(http.StatusOK)
		}))
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		request.Header.Set("Authorization", "junk")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		gate.mu.Lock()
		proven, unproven := stream.proven, len(gate.unproven)
		gate.mu.Unlock()
		if proven != verified || (unproven == 0) != verified {
			t.Fatalf("verified=%v: proven=%v unproven=%d", verified, proven, unproven)
		}
	}
}

type nopConn struct{ net.Conn }

func (*nopConn) Close() error { return nil }
