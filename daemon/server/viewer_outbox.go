package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
)

type screenMessage struct {
	kind int
	data []byte
}

// viewerOutbox writes a viewer socket from one goroutine, so nothing that
// sends to the viewer (the encode loop's stats and signalling, input
// handling) waits on a slow socket. A failed write ends the view. When the
// view ends, what is queued (such as the error that ended it) is flushed and
// the socket is closed with closeReason's words.
type viewerOutbox struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	ch     chan screenMessage
	done   chan struct{}
}

func startViewerOutbox(ctx context.Context, cancel context.CancelCauseFunc, conn *websocket.Conn, closeReason func(cause error) string) *viewerOutbox {
	o := &viewerOutbox{ctx: ctx, cancel: cancel, ch: make(chan screenMessage, 32), done: make(chan struct{})}
	go func() {
		defer close(o.done)
		for {
			select {
			case <-ctx.Done():
				for drained := false; !drained; {
					select {
					case msg := <-o.ch:
						_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
						if conn.WriteMessage(msg.kind, msg.data) != nil {
							drained = true
						}
					default:
						drained = true
					}
				}
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, closeReason(context.Cause(ctx))), time.Now().Add(time.Second))
				return
			case msg := <-o.ch:
				_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if err := conn.WriteMessage(msg.kind, msg.data); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return o
}

// sendJSON queues a message the viewer needs, waiting for room; false when
// the view has ended.
func (o *viewerOutbox) sendJSON(v any) bool {
	raw, _ := json.Marshal(v)
	select {
	case o.ch <- screenMessage{kind: websocket.TextMessage, data: raw}:
		return true
	case <-o.ctx.Done():
		return false
	}
}

// advise queues what a viewer can do without (stats, the lock), dropping it
// when the socket is behind.
func (o *viewerOutbox) advise(v any) {
	raw, _ := json.Marshal(v)
	select {
	case o.ch <- screenMessage{kind: websocket.TextMessage, data: raw}:
	default:
	}
}

// wait ends the view and waits for the socket's last writes.
func (o *viewerOutbox) wait() {
	o.cancel(nil)
	<-o.done
}
