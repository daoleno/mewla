package server

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
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
	kind        string // wire "type" of a JSON message, for the byte tally
	queuedAt    time.Time
}

// Bulk messages wait while anything else is queued: Work and Calendar (their
// snapshots are some 300 KB of the bundle every connection starts with, and
// their changes ride with them so a change never overtakes the snapshot it
// follows) and pictures (codex_asset, hundreds of KB each). On a slow link the
// thread a person just opened, a Session's news and replies to what they typed
// go first. Once bulkShare bytes of other messages went out since the last bulk
// one, the next bulk message goes regardless, so a busy link still delivers it.
var bulkShare = 256 << 10

// wsNotSentLowat is how much a client's socket may hold unsent
// (keepBacklogInOutbox): a quarter second of a 60 KB/s link.
const wsNotSentLowat = 16 << 10

// The Work and Calendar snapshots (every connection starts with them, and an
// app may ask again) also wait for the connection's first thread page: an app
// opens on Brain, and on a slow link those 300 KB would otherwise go out first
// and leave Brain blank for seconds. A connection that opens no thread gets
// them threadWait after it opened.
var threadWait = 1500 * time.Millisecond

func waitsForFirstThread(kind string) bool {
	return kind == "work_items_snapshot" || kind == "calendar_items_snapshot"
}

func isBulkMessage(kind string) bool {
	return kind == "codex_asset" || strings.HasPrefix(kind, "work_item") || strings.HasPrefix(kind, "calendar_item")
}

// typeTally is what one message type has cost one client.
type typeTally struct {
	bytes int64
	count int
}

// clientOutbox is one WebSocket client's ordered send queue. Broadcasts,
// request handlers and terminal streams enqueue; a single writer goroutine owns
// every write to the socket. A client that stops reading therefore costs only
// its own connection.
type clientOutbox struct {
	conn  *websocket.Conn
	mu    sync.Mutex
	cond  *sync.Cond
	queue []outboundMessage
	bulk  []outboundMessage // isBulkMessage, in order; sent when queue is empty
	bytes int               // queued plus in-flight

	openedAt   time.Time
	threadSent bool // a codex_conversation_snapshot went out (waitsForFirstThread)
	sinceBulk  int  // bytes of other messages sent while a bulk one was ready
	closed     bool
	err        error

	sent         int64 // bytes written to the socket so far
	byType       map[string]typeTally
	lastSendAt   time.Time
	lastSendSize int
	lastSendTook time.Duration
	failure      string // why the writer gave up on this client, if it did

	// frameDeflate: the client offered frameDeflateSubprotocol, so the writer
	// sends JSON text of frameDeflateMin bytes or more as one binary message
	// of its raw DEFLATE bytes. wire counts the bytes that went out.
	frameDeflate bool
	wire         int64
	deflater     *flate.Writer
	deflated     bytes.Buffer
}

// frameDeflateSubprotocol is the WebSocket subprotocol a client offers, beside
// its auth subprotocol, to say it inflates binary messages: each one is a JSON
// message as raw DEFLATE (RFC 1951). It is for clients whose WebSocket cannot
// negotiate permessage-deflate (NSH's native one): a 750 KB thread snapshot is
// some 250 KB on the wire, and a phone behind a slow tunnel waits on every
// byte. The daemon never selects it (the handshake echoes the auth
// subprotocol); a client that did not offer it gets text as before.
const frameDeflateSubprotocol = "mewla.frames.deflate-raw"

// frameDeflateMin is the smallest message the writer deflates: below it, the
// bytes saved do not pay for the work on either end.
const frameDeflateMin = 1024

// clientOffersFrameDeflate reports whether the upgrade offered
// frameDeflateSubprotocol.
func clientOffersFrameDeflate(r *http.Request) bool {
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == frameDeflateSubprotocol {
			return true
		}
	}
	return false
}

// useFrameDeflate turns deflated frames on, before the first message is queued.
func (o *clientOutbox) useFrameDeflate() {
	o.mu.Lock()
	o.frameDeflate = true
	o.mu.Unlock()
}

// wireMessage is msg as it goes out: deflated into a binary message when the
// client asked for that and it is large enough, else unchanged. Only the
// writer goroutine calls it.
func (o *clientOutbox) wireMessage(msg outboundMessage, deflate bool) outboundMessage {
	if !deflate || msg.messageType != websocket.TextMessage || len(msg.data) < frameDeflateMin {
		return msg
	}
	o.deflated.Reset()
	if o.deflater == nil {
		o.deflater, _ = flate.NewWriter(&o.deflated, flate.DefaultCompression)
	} else {
		o.deflater.Reset(&o.deflated)
	}
	if _, err := o.deflater.Write(msg.data); err != nil {
		return msg
	}
	if err := o.deflater.Close(); err != nil {
		return msg
	}
	return outboundMessage{messageType: websocket.BinaryMessage, data: append([]byte(nil), o.deflated.Bytes()...), kind: msg.kind}
}

// outboxStats is what the writer has done for one client, kept so a dropped
// connection can say whether it died mid-send.
type outboxStats struct {
	sent         int64
	wire         int64 // bytes written; sent counts them before frame deflate
	frameDeflate bool
	byType       []typeStat // heaviest first
	pending      int
	lastSendAt   time.Time
	lastSendSize int
	lastSendTook time.Duration
	failure      string
}

func (o *clientOutbox) stats() outboxStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return outboxStats{sent: o.sent, wire: o.wire, frameDeflate: o.frameDeflate, byType: sortedTally(o.byType), pending: o.bytes, lastSendAt: o.lastSendAt, lastSendSize: o.lastSendSize, lastSendTook: o.lastSendTook, failure: o.failure}
}

func newClientOutbox(conn *websocket.Conn) *clientOutbox {
	o := &clientOutbox{conn: conn, openedAt: time.Now()}
	o.cond = sync.NewCond(&o.mu)
	time.AfterFunc(threadWait, func() {
		o.mu.Lock()
		o.cond.Broadcast()
		o.mu.Unlock()
	})
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
	msg := outboundMessage{messageType: messageType, data: data, kind: wireMessageType(messageType, data), queuedAt: time.Now()}
	if isBulkMessage(msg.kind) {
		o.bulk = append(o.bulk, msg)
	} else {
		o.queue = append(o.queue, msg)
	}
	o.bytes += len(data)
	o.cond.Broadcast()
	return nil
}

func (o *clientOutbox) run() {
	for {
		o.mu.Lock()
		for !o.closed && len(o.queue) == 0 && !o.bulkReadyLocked() {
			o.cond.Wait()
		}
		if o.closed {
			o.mu.Unlock()
			return
		}
		var msg outboundMessage
		if ready := o.bulkReadyLocked(); len(o.queue) > 0 && (!ready || o.sinceBulk < bulkShare) {
			msg, o.queue = o.queue[0], o.queue[1:]
			if ready {
				o.sinceBulk += len(msg.data)
			}
		} else {
			msg, o.bulk = o.bulk[0], o.bulk[1:]
			o.sinceBulk = 0
		}
		deflate := o.frameDeflate
		o.mu.Unlock()

		started := time.Now()
		out := o.wireMessage(msg, deflate)
		err := writeWithProgressDeadline(o.conn, out)
		o.mu.Lock()
		if err != nil {
			o.failLocked(err)
		} else {
			o.sent += int64(len(msg.data))
			o.wire += int64(len(out.data))
			if o.byType == nil {
				o.byType = make(map[string]typeTally)
			}
			tally := o.byType[msg.kind]
			tally.bytes += int64(len(msg.data))
			tally.count++
			o.byType[msg.kind] = tally
			o.lastSendAt, o.lastSendSize, o.lastSendTook = time.Now(), len(msg.data), time.Since(started)
			if msg.kind == "codex_conversation_snapshot" {
				o.threadSent = true
			}
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

// bulkReadyLocked reports whether the bulk lane's next message may go: it has
// one, and it is not a Work or Calendar snapshot still waiting for the first
// thread page (which holds the bulk messages behind it too, in order).
func (o *clientOutbox) bulkReadyLocked() bool {
	if len(o.bulk) == 0 {
		return false
	}
	return !waitsForFirstThread(o.bulk[0].kind) || o.threadSent || time.Since(o.openedAt) >= threadWait
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
	o.bulk = nil
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

	// deflate is whether the client offered permessage-deflate, so JSON text
	// goes out compressed; sent= and types= count the bytes before it.
	deflate bool

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
	frames := ""
	if out.frameDeflate {
		frames = fmt.Sprintf(" frames=deflate-raw wire=%dB", out.wire)
	}
	return fmt.Sprintf("sent=%dB pending=%dB lastSend=%dB/%s took %s pongs=%d/%d lastPong=%s deflate=%t%s types=%s",
		out.sent, out.pending, out.lastSendSize, since(out.lastSendAt), out.lastSendTook.Round(time.Millisecond), pongs, pings, since(lastPongAt), l.deflate, frames, describeTally(out.byType, out.sent))
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

// typeStat is one message type's share of what a client was sent.
type typeStat struct {
	kind  string
	bytes int64
	count int
}

func sortedTally(by map[string]typeTally) []typeStat {
	stats := make([]typeStat, 0, len(by))
	for kind, tally := range by {
		stats = append(stats, typeStat{kind, tally.bytes, tally.count})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].bytes != stats[j].bytes {
			return stats[i].bytes > stats[j].bytes
		}
		return stats[i].kind < stats[j].kind
	})
	return stats
}

// wsQuietCloseLogBytes is how much a client must have been sent before even a
// clean close is logged: a slow link that carried this much explains a slow
// load, and the line's types= says what filled it.
const wsQuietCloseLogBytes = 1 << 20

// wsTallyShown is how many message types a connection-lost line names; the
// rest are summed as "other".
const wsTallyShown = 5

// describeTally renders the heaviest message types as kind:bytes/count, so a
// slow link's log says what filled it.
func describeTally(stats []typeStat, sent int64) string {
	if len(stats) == 0 {
		return "none"
	}
	var b strings.Builder
	var shown int64
	for i, stat := range stats {
		if i == wsTallyShown {
			break
		}
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%dB/%d", stat.kind, stat.bytes, stat.count)
		shown += stat.bytes
	}
	if rest := sent - shown; rest > 0 {
		fmt.Fprintf(&b, ",other:%dB/%d", rest, len(stats)-wsTallyShown)
	}
	return b.String()
}

// wireMessageType reads the top-level "type" of a JSON message without
// decoding it: snapshots are megabytes, and every queued message passes
// here. Binary frames, and JSON with no string "type", name themselves.
func wireMessageType(messageType int, data []byte) string {
	if messageType != websocket.TextMessage {
		return "binary"
	}
	i := 0
	for i < len(data) && data[i] != '{' {
		if data[i] != ' ' && data[i] != '\n' && data[i] != '\t' && data[i] != '\r' {
			return "unknown"
		}
		i++
	}
	depth := 0
	for ; i < len(data); i++ {
		switch data[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '"':
			start := i + 1
			for i++; i < len(data) && data[i] != '"'; i++ {
				if data[i] == '\\' {
					i++
				}
			}
			if i >= len(data) {
				return "unknown"
			}
			if depth != 1 || string(data[start:i]) != "type" {
				continue
			}
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == ':') {
				j++
			}
			if j >= len(data) || data[j] != '"' {
				continue // a key named "type" whose value is not a string
			}
			end := j + 1
			for end < len(data) && data[end] != '"' && data[end] != '\\' {
				end++
			}
			if end >= len(data) || data[end] != '"' {
				return "unknown"
			}
			return string(data[j+1 : end])
		}
	}
	return "unknown"
}

// clientOffersDeflate reports whether the upgrade asked for permessage-deflate,
// which the upgrader accepts (EnableCompression).
func clientOffersDeflate(r *http.Request) bool {
	for _, value := range r.Header.Values("Sec-WebSocket-Extensions") {
		if strings.Contains(strings.ToLower(value), "permessage-deflate") {
			return true
		}
	}
	return false
}
