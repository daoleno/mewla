package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/pi"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/google/uuid"
)

// piAckWait bounds how long a sent message waits for the extension to say it
// handed the message to Pi.
const defaultPiAckWait = 5 * time.Second

// piEvents is the daemon end of the extension Mewla loads into every Pi it
// starts. Each extension connects to one local socket, names its session file
// (which Mewla chose at launch, so it names the Worker), and from then on
// reports Pi's lifecycle and takes a message or a stop.
type piEvents struct {
	ctx     context.Context
	hub     *agentevents.Hub
	now     func() time.Time
	workers func() []*classifier.Worker
	// ackWait bounds the wait for the extension's acknowledgement.
	ackWait time.Duration

	mu    sync.Mutex
	conns map[string]*piConn
}

type piConn struct {
	owner    *piEvents
	conn     net.Conn
	workerID string
	mapper   *pi.Mapper

	writeMu sync.Mutex
	mu      sync.Mutex
	acks    map[string]chan error
}

func newPiEvents(ctx context.Context, hub *agentevents.Hub, workers func() []*classifier.Worker) *piEvents {
	return &piEvents{ctx: ctx, hub: hub, now: time.Now, workers: workers, ackWait: defaultPiAckWait, conns: map[string]*piConn{}}
}

// Listen accepts extension connections on a socket only this user can open.
func (p *piEvents) Listen(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return err
	}
	go func() {
		<-p.ctx.Done()
		listener.Close()
		_ = os.Remove(path)
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go p.serve(conn)
		}
	}()
	return nil
}

// workerFor is the Worker Pi was launched with the session file for.
func (p *piEvents) workerFor(session string) string {
	if session == "" {
		return ""
	}
	for _, w := range p.workers() {
		if w != nil && work.PiOwnedSessionPath(w.Command) == session {
			return w.ID
		}
	}
	return ""
}

func (p *piEvents) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, 64*1024)
	var c *piConn
	defer func() {
		if c != nil {
			p.drop(c)
		}
	}()
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		msg, err := pi.ParseMessage(line)
		if err != nil {
			continue
		}
		at := p.now().UTC()
		if msg.T == "hello" {
			if c == nil {
				workerID := p.workerFor(msg.Session)
				if workerID == "" {
					// Not (yet) a Worker Mewla launched; the extension asks again.
					return
				}
				c = &piConn{owner: p, conn: conn, workerID: workerID, mapper: pi.NewMapper(workerID), acks: map[string]chan error{}}
				p.attach(c)
			}
			launch := ""
			for _, w := range p.workers() {
				if w != nil && w.ID == c.workerID {
					launch = work.PiOwnedSessionPath(w.Command)
				}
			}
			binding := pi.Binding(msg, launch)
			p.hub.Publish(agentevents.Event{WorkerID: c.workerID, Kind: agentevents.KindBound, At: at, Binding: &binding})
			continue
		}
		if c == nil {
			continue
		}
		switch msg.T {
		case "sent":
			c.ack(msg.ID, nil)
		case "send_failed":
			c.ack(msg.ID, errors.New(msg.Error))
		default:
			for _, e := range c.mapper.Map(msg, at) {
				p.hub.Publish(e)
			}
		}
	}
}

func (p *piEvents) attach(c *piConn) {
	p.mu.Lock()
	old := p.conns[c.workerID]
	p.conns[c.workerID] = c
	p.mu.Unlock()
	if old != nil {
		_ = old.conn.Close()
	}
	p.hub.SetConnected(c.workerID, true)
}

func (p *piEvents) drop(c *piConn) {
	p.mu.Lock()
	current := p.conns[c.workerID] == c
	if current {
		delete(p.conns, c.workerID)
	}
	p.mu.Unlock()
	if current {
		p.hub.SetConnected(c.workerID, false)
	}
}

func (p *piEvents) conn(workerID string) *piConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns[workerID]
}

func (c *piConn) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = c.conn.Write(append(raw, '\n'))
	return err
}

func (c *piConn) ack(id string, err error) {
	c.mu.Lock()
	ch := c.acks[id]
	delete(c.acks, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- err
	}
}

// send hands a message to Pi through its extension. handled is false for a
// Worker with no live extension, which is typed into its pane instead. A
// message that could not be written was not sent; one written and not
// acknowledged may have been, and is left ambiguous.
func (p *piEvents) send(sessionID, payload, receipt string, begin func() error) (handled bool, err error) {
	c := p.conn(sessionID)
	if c == nil {
		return false, nil
	}
	if receipt == "" {
		receipt = uuid.NewString()
	}
	if err := begin(); err != nil {
		return true, err
	}
	ack := make(chan error, 1)
	c.mu.Lock()
	c.acks[receipt] = ack
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.acks, receipt)
		c.mu.Unlock()
	}()
	if err := c.write(map[string]string{"t": "send", "id": receipt, "text": payload}); err != nil {
		return true, &watcher.StructuredInputError{Err: err, NotSent: true}
	}
	select {
	case err := <-ack:
		if err != nil {
			// The extension refused it before Pi took it.
			return true, &watcher.StructuredInputError{Err: err, NotSent: true}
		}
		return true, nil
	case <-time.After(p.ackWait):
		return true, &watcher.StructuredInputError{Err: errors.New("pi did not acknowledge the message")}
	case <-p.ctx.Done():
		return true, p.ctx.Err()
	}
}

// interrupt stops the Worker's running turn through the extension. A Worker
// with no running turn has nothing to stop, and gets no keystroke either.
func (p *piEvents) interrupt(sessionID string) (handled bool, err error) {
	c := p.conn(sessionID)
	if c == nil {
		return false, nil
	}
	state, ok := p.hub.State(sessionID)
	if !ok || state.Current == nil || state.Current.Status != agentevents.TurnRunning {
		return true, nil
	}
	return true, c.write(map[string]string{"t": "abort"})
}

// Observe lets go of a Worker that is gone.
func (p *piEvents) Observe(event watcher.SessionEvent) {
	if event.Type != "worker_removed" {
		return
	}
	if c := p.conn(event.WorkerID); c != nil {
		_ = c.conn.Close()
	}
}

func logPiListen(err error) {
	if err != nil {
		log.Printf("WARN agent events: cannot listen for Pi, Pi Workers will be followed from their transcript: %v", err)
	}
}
