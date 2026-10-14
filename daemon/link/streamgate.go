package link

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

const (
	defaultMaxUnprovenStreams = 8
	defaultUnprovenLifetime   = 30 * time.Second
)

// streamGate bounds streams that have not yet carried an authenticated
// request. Anyone who knows a route's hostname can open a stream, so without
// this a few slow unauthenticated requests hold every stream slot. When the
// unproven slots are full the oldest unproven stream of the client holding
// the most is closed: a paired device proves its stream with its first
// request, and a browser waiting for enrollment keeps its streams while
// another client churns, so holding slots open gains an attacker nothing.
type streamGate struct {
	mu       sync.Mutex
	max      int
	lifetime time.Duration
	unproven []*gatedStream
}

func newStreamGate(maxUnproven int, lifetime time.Duration) *streamGate {
	return &streamGate{max: maxUnproven, lifetime: lifetime}
}

// admit tracks conn from client (a clientaddr key) as unproven, closing it after the unproven lifetime unless
// a request on it proves a paired device.
func (gate *streamGate) admit(conn net.Conn, client string) *gatedStream {
	stream := &gatedStream{gate: gate, conn: conn, client: client}
	gate.mu.Lock()
	var evicted *gatedStream
	if len(gate.unproven) >= gate.max {
		index := gate.evictionIndexLocked(client)
		evicted = gate.unproven[index]
		gate.unproven = append(gate.unproven[:index], gate.unproven[index+1:]...)
		evicted.released = true
	}
	gate.unproven = append(gate.unproven, stream)
	gate.mu.Unlock()
	if evicted != nil {
		_ = evicted.conn.Close()
	}
	stream.timer = time.AfterFunc(gate.lifetime, func() {
		gate.mu.Lock()
		proven := stream.proven
		gate.mu.Unlock()
		if !proven {
			_ = conn.Close()
		}
	})
	return stream
}

// evictionIndexLocked picks the oldest unproven stream of the client with the
// most unproven streams, counting the arriving stream for its client.
func (gate *streamGate) evictionIndexLocked(arriving string) int {
	counts := map[string]int{arriving: 1}
	for _, stream := range gate.unproven {
		counts[stream.client]++
	}
	most := 0
	for _, count := range counts {
		most = max(most, count)
	}
	for index, stream := range gate.unproven {
		if counts[stream.client] == most {
			return index
		}
	}
	return 0
}

type gatedStream struct {
	gate     *streamGate
	conn     net.Conn
	client   string
	timer    *time.Timer
	proven   bool
	released bool
}

func (stream *gatedStream) prove() {
	stream.gate.mu.Lock()
	defer stream.gate.mu.Unlock()
	if stream.proven {
		return
	}
	stream.proven = true
	stream.timer.Stop()
	stream.releaseLocked()
}

func (stream *gatedStream) close() {
	stream.gate.mu.Lock()
	defer stream.gate.mu.Unlock()
	stream.timer.Stop()
	stream.releaseLocked()
}

func (stream *gatedStream) releaseLocked() {
	if stream.released {
		return
	}
	stream.released = true
	for index, current := range stream.gate.unproven {
		if current == stream {
			stream.gate.unproven = append(stream.gate.unproven[:index], stream.gate.unproven[index+1:]...)
			return
		}
	}
}

// handler marks the stream proven once the server verifies a paired
// device's signature or capability on one of its requests.
func (stream *gatedStream) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request.WithContext(auth.WithAuthenticatedHook(request.Context(), stream.prove)))
	})
}
