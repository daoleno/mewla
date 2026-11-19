package server

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// viewerReadIdle is how long a viewer socket may stay silent before the view
// ends; the app pings well within it.
const viewerReadIdle = 45 * time.Second

// viewerReads is a viewer socket's read loop, owned by its handler: stop
// (deferred by the handler) ends the read and waits for the loop, so nothing
// the loop does, such as verifying the viewer's signed answer (which records
// the device as seen), runs on after the handler has returned.
type viewerReads struct {
	conn   *websocket.Conn
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	ended  bool
	done   chan struct{}
}

// readViewer runs loop on its own goroutine. loop calls next for each
// message and returns when next fails.
func readViewer(conn *websocket.Conn, cancel context.CancelCauseFunc, loop func(next func() (int, []byte, error))) *viewerReads {
	r := &viewerReads{conn: conn, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		loop(r.next)
	}()
	return r
}

// next reads the next message, failing once the view has ended.
func (r *viewerReads) next() (int, []byte, error) {
	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		return 0, nil, context.Canceled
	}
	_ = r.conn.SetReadDeadline(time.Now().Add(viewerReadIdle))
	r.mu.Unlock()
	return r.conn.ReadMessage()
}

// stop ends the view, unblocks a read in progress and waits for the loop.
func (r *viewerReads) stop() {
	r.cancel(nil)
	r.mu.Lock()
	r.ended = true
	_ = r.conn.SetReadDeadline(time.Now())
	r.mu.Unlock()
	<-r.done
}
