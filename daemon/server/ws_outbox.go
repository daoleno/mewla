package server

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
	"github.com/gorilla/websocket"
)

// wsWriteTimeout bounds how long one client may go without accepting the next
// write chunk. A client that stops reading (a suspended phone, a dead proxy
// leg) fails its own writer within this deadline and is disconnected; nobody
// else waits on it. A slow link that keeps draining is never cut off mid
// message, however large the message.
var wsWriteTimeout = 30 * time.Second

// wsWriteChunk is the progress unit for wsWriteTimeout: each chunk of a
// message gets a fresh deadline.
var wsWriteChunk = 32 << 10

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

	sent         int64 // bytes written to the socket so far
	lastSendAt   time.Time
	lastSendSize int
	lastSendTook time.Duration
	failure      string // why the writer gave up on this client, if it did
}

// outboxStats is what the writer has done for one client, kept so a dropped
// connection can say whether it died mid-send.
type outboxStats struct {
	sent         int64
	pending      int
	lastSendAt   time.Time
	lastSendSize int
	lastSendTook time.Duration
	failure      string
}

func (o *clientOutbox) stats() outboxStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return outboxStats{o.sent, o.bytes, o.lastSendAt, o.lastSendSize, o.lastSendTook, o.failure}
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
			started := time.Now()
			err := writeWithProgressDeadline(o.conn, msg)
			o.mu.Lock()
			if err != nil {
				o.failLocked(err)
			} else {
				o.sent += int64(len(msg.data))
				o.lastSendAt, o.lastSendSize, o.lastSendTook = time.Now(), len(msg.data), time.Since(started)
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

// writeWithProgressDeadline writes one message in chunks and renews the write
// deadline before each, so the deadline measures progress rather than the
// time a whole message takes on a slow link.
// Only JSON text is compressed when the client negotiated permessage-deflate;
// terminal bytes and browser frames are binary and gain little.
func writeWithProgressDeadline(conn *websocket.Conn, msg outboundMessage) error {
	conn.EnableWriteCompression(msg.messageType == websocket.TextMessage)
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	if len(msg.data) <= wsWriteChunk {
		return conn.WriteMessage(msg.messageType, msg.data)
	}
	w, err := conn.NextWriter(msg.messageType)
	if err != nil {
		return err
	}
	for data := msg.data; len(data) > 0; {
		n := min(len(data), wsWriteChunk)
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		if _, err := w.Write(data[:n]); err != nil {
			_ = w.Close()
			return err
		}
		data = data[n:]
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return w.Close()
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
	o.failure = err.Error()
	diag.Report("server.websocket.dropped", diag.SeverityWarn, "", fmt.Sprintf("client %s dropped with %d bytes pending: %v", o.conn.RemoteAddr(), o.bytes, err))
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

// wsPingInterval is how often the daemon pings a client. The ping keeps an
// otherwise quiet link (a phone on a relay) from idling out, and its pong is
// how the daemon learns the client is still there.
var wsPingInterval = 20 * time.Second

// wsReadIdleTimeout is how long a client may send nothing, not even a pong,
// before its connection is dropped. A phone that vanished without a close (a
// network switch, a suspended app) otherwise stays "connected" here until the
// kernel notices, and its input resends would queue behind a dead socket.
var wsReadIdleTimeout = 60 * time.Second

// wsLink is the daemon's view of one client's socket: when the client last
// answered a ping, and why the daemon itself closed the socket, if it did. A
// socket the daemon closes ends its read loop with an error that looks like any
// other; this is what tells the log which end hung up.
type wsLink struct {
	conn     *websocket.Conn
	openedAt time.Time

	mu         sync.Mutex
	pings      int
	pongs      int
	lastPongAt time.Time
	closedBy   string

	done chan struct{}
	once sync.Once
}

func (l *wsLink) stop() { l.once.Do(func() { close(l.done) }) }

// closeBecause ends the socket from the daemon's side and remembers why.
func (l *wsLink) closeBecause(reason string) {
	l.mu.Lock()
	if l.closedBy == "" {
		l.closedBy = reason
	}
	l.mu.Unlock()
	_ = l.conn.Close()
}

// endCause names what ended a read loop: the daemon closing the socket, the
// client going silent past wsReadIdleTimeout, or the client's side vanishing.
func (l *wsLink) endCause(readErr error, out outboxStats) string {
	l.mu.Lock()
	closedBy := l.closedBy
	l.mu.Unlock()
	switch {
	case out.failure != "":
		return "daemon-closed(outbox: " + out.failure + ")"
	case closedBy != "":
		return "daemon-closed(" + closedBy + ")"
	case isTemporaryNetError(readErr):
		return "client-silent"
	default:
		return "peer-closed"
	}
}

// describe summarizes the link for a log line: how much was sent, how the last
// send went, and how recently the client answered a ping.
func (l *wsLink) describe(out outboxStats) string {
	l.mu.Lock()
	pings, pongs, lastPongAt := l.pings, l.pongs, l.lastPongAt
	l.mu.Unlock()
	now := time.Now()
	since := func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return now.Sub(t).Round(10 * time.Millisecond).String()
	}
	return fmt.Sprintf("sent=%dB pending=%dB lastSend=%dB/%s took %s pongs=%d/%d lastPong=%s",
		out.sent, out.pending, out.lastSendSize, since(out.lastSendAt), out.lastSendTook.Round(time.Millisecond), pongs, pings, since(lastPongAt))
}

// keepWebSocketAlive pings conn on a timer and drops it when the client goes
// silent past wsReadIdleTimeout. stop ends the pinger. Client frames and pongs
// both renew the deadline; the read loop renews it after every message, and
// this installs the control-frame handlers it cannot see.
func keepWebSocketAlive(conn *websocket.Conn) *wsLink {
	interval, idle := wsPingInterval, wsReadIdleTimeout
	renew := func() { _ = conn.SetReadDeadline(time.Now().Add(idle)) }
	renew()
	link := &wsLink{conn: conn, openedAt: time.Now(), done: make(chan struct{})}
	conn.SetPongHandler(func(string) error {
		link.mu.Lock()
		link.pongs++
		link.lastPongAt = time.Now()
		link.mu.Unlock()
		renew()
		return nil
	})
	conn.SetPingHandler(func(data string) error {
		renew()
		err := conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
		if err == websocket.ErrCloseSent || isTemporaryNetError(err) {
			return nil
		}
		return err
	})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-link.done:
				return
			case <-ticker.C:
				link.mu.Lock()
				link.pings++
				link.mu.Unlock()
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					link.closeBecause("ping not sent: " + err.Error())
					return
				}
			}
		}
	}()
	return link
}

func isTemporaryNetError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
