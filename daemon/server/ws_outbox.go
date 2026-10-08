package server

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsWriteTimeout bounds one message to one client. A client that stops
// reading (a suspended phone, a dead proxy leg) fails its own writer within
// this deadline and is disconnected; nobody else waits on it.
var wsWriteTimeout = 30 * time.Second

// wsOutboxLimit bounds the bytes queued for one client. A client this far
// behind is closed and reconnects to a fresh snapshot. Producers that wait for
// room (terminal streams) wait at half the limit, so a busy terminal on a slow
// link never pushes a broadcast over the edge.
var wsOutboxLimit = 16 << 20

var (
	errOutboxClosed   = errors.New("connection is closed")
	errOutboxOverflow = errors.New("client fell too far behind")
)

type outboundMessage struct {
	messageType int
	data        []byte
}

// clientOutbox is one WebSocket client's ordered send queue. Broadcasts,
// request handlers and terminal streams enqueue; a single writer goroutine owns
// every write to the socket. A client that stops reading therefore costs only
// its own connection.
type clientOutbox struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	cond   *sync.Cond
	queue  []outboundMessage
	bytes  int // queued plus in-flight
	closed bool
	err    error
}

func newClientOutbox(conn *websocket.Conn) *clientOutbox {
	o := &clientOutbox{conn: conn}
	o.cond = sync.NewCond(&o.mu)
	go o.run()
	return o
}

// enqueue queues one message without touching the socket. When the client is
// too far behind, a non-waiting producer closes it; a waiting producer blocks
// until the writer makes room or the connection closes.
func (o *clientOutbox) enqueue(messageType int, data []byte, waitForRoom bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if waitForRoom {
		for !o.closed && o.bytes > 0 && o.bytes+len(data) > wsOutboxLimit/2 {
			o.cond.Wait()
		}
	} else if !o.closed && o.bytes > 0 && o.bytes+len(data) > wsOutboxLimit {
		o.failLocked(errOutboxOverflow)
	}
	if o.closed {
		return o.err
	}
	o.queue = append(o.queue, outboundMessage{messageType: messageType, data: data})
	o.bytes += len(data)
	o.cond.Broadcast()
	return nil
}

func (o *clientOutbox) run() {
	for {
		o.mu.Lock()
		for !o.closed && len(o.queue) == 0 {
			o.cond.Wait()
		}
		if o.closed {
			o.mu.Unlock()
			return
		}
		batch := o.queue
		o.queue = nil
		o.mu.Unlock()

		for _, msg := range batch {
			_ = o.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			err := o.conn.WriteMessage(msg.messageType, msg.data)
			o.mu.Lock()
			if err != nil {
				o.failLocked(err)
			}
			o.bytes -= len(msg.data)
			o.cond.Broadcast()
			closed := o.closed
			o.mu.Unlock()
			if closed {
				return
			}
		}
	}
}

// close stops the writer and wakes waiting producers. Detach owns closing the
// socket itself.
func (o *clientOutbox) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeLocked(errOutboxClosed)
}

// failLocked drops a client that cannot keep up. Closing the socket unblocks a
// write in progress and ends the client's read loop, which detaches it.
func (o *clientOutbox) failLocked(err error) {
	if o.closed {
		return
	}
	log.Printf("websocket client %s dropped with %d bytes pending: %v", o.conn.RemoteAddr(), o.bytes, err)
	o.closeLocked(fmt.Errorf("%w: %v", errOutboxClosed, err))
	_ = o.conn.Close()
}

func (o *clientOutbox) closeLocked(err error) {
	if o.closed {
		return
	}
	o.closed = true
	o.err = err
	o.queue = nil
	o.cond.Broadcast()
}
