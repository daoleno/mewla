package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/codex"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/watcher"
)

const (
	// codexConnectWindow bounds how long a new Worker's app-server is waited
	// for: the TUI starts it a moment before the first connection can land.
	codexConnectWindow = 90 * time.Second
	// codexAttachTries and codexAttachGap bound the attach that follows the
	// thread going active: thread/resume fails until the first turn's rollout
	// exists, which the spike measured at about half a second.
	codexAttachTries = 20
	codexAttachGap   = 250 * time.Millisecond
)

// codexEvents watches the app-server of every Codex Worker launched with one
// and feeds what it says into the Hub.
type codexEvents struct {
	ctx      context.Context
	hub      *agentevents.Hub
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]*codexSession
}

func newCodexEvents(ctx context.Context, hub *agentevents.Hub) *codexEvents {
	return &codexEvents{ctx: ctx, hub: hub, now: time.Now, sessions: map[string]*codexSession{}}
}

// Observe follows the Watcher: a Worker with a control socket gets a session,
// and a Worker that is gone takes its app-server with it.
func (c *codexEvents) Observe(event watcher.SessionEvent) {
	switch event.Type {
	case "worker_discovered":
		if event.Worker != nil && event.Worker.AgentControl != "" {
			c.start(event.Worker.ID, event.Worker.AgentControl)
		}
	case "worker_removed":
		c.stop(event.WorkerID)
		if event.Worker != nil && event.Worker.AgentControl != "" {
			_ = modelprofiles.CleanupCodexControlArtifacts(event.Worker.AgentControl)
		}
	}
}

func (c *codexEvents) start(workerID, socket string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.sessions[workerID]; existing != nil && existing.socket == socket {
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	s := &codexSession{owner: c, workerID: workerID, socket: socket, cancel: cancel, mapper: codex.NewMapper(workerID)}
	c.sessions[workerID] = s
	go s.run(ctx)
}

func (c *codexEvents) stop(workerID string) {
	c.mu.Lock()
	s := c.sessions[workerID]
	delete(c.sessions, workerID)
	c.mu.Unlock()
	if s != nil {
		s.cancel()
	}
}

// client returns the live connection of a Worker's session, if it has one.
func (c *codexEvents) client(workerID string) (*codexctl.Client, string) {
	c.mu.Lock()
	s := c.sessions[workerID]
	c.mu.Unlock()
	if s == nil {
		return nil, ""
	}
	return s.connection()
}

type codexSession struct {
	owner    *codexEvents
	workerID string
	socket   string
	cancel   context.CancelFunc
	mapper   *codex.Mapper

	mu     sync.Mutex
	client *codexctl.Client
	thread string
}

func (s *codexSession) connection() (*codexctl.Client, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client, s.thread
}

func (s *codexSession) publish(events ...agentevents.Event) {
	for _, e := range events {
		s.owner.hub.Publish(e)
	}
}

func (s *codexSession) run(ctx context.Context) {
	client := s.dial(ctx)
	if client == nil {
		return
	}
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.client = nil
		s.mu.Unlock()
		_ = client.Close()
	}()
	decided, cancelSub := s.owner.hub.Subscribe(s.workerID)
	defer cancelSub()

	pending := map[string]json.RawMessage{}
	attached := map[string]bool{}
	attachResult := make(chan attachOutcome, 1)
	attaching := false
	attach := func(thread string, tries int) {
		if attaching || attached[thread] {
			return
		}
		attaching = true
		go func() { attachResult <- s.attach(ctx, client, thread, tries) }()
	}

	// A daemon that restarted, or a Worker found after its TUI started, meets
	// a thread that already exists: the server announced it before this
	// connection. Name it, and attach once if it has run a turn already; a
	// thread that has not has no rollout to attach to, and is attached when
	// it first goes active.
	if ids, err := client.LoadedThreadIDs(ctx); err == nil && len(ids) > 0 {
		if id, err := client.ResolveThread(ctx, ""); err == nil {
			known, _ := json.Marshal(map[string]any{"thread": map[string]any{"id": id, "ephemeral": false}})
			s.publish(s.mapper.Notification("thread/started", known, s.owner.now().UTC())...)
			s.setThread(s.mapper.Thread())
			attach(id, 1)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-client.Notifications():
			if !ok {
				s.publish(agentevents.Event{WorkerID: s.workerID, Kind: agentevents.KindExit, At: s.owner.now().UTC()})
				return
			}
			s.publish(s.mapper.Notification(notification.Method, notification.Params, s.owner.now().UTC())...)
			if thread := s.mapper.Thread(); thread != "" {
				s.setThread(thread)
				if s.mapper.Active() {
					attach(thread, codexAttachTries)
				}
			}
		case request, ok := <-client.ServerRequests():
			if !ok {
				return
			}
			if approval, carried := s.mapper.Approval(request.ID, request.Method, request.Params); carried {
				pending[approval.ID] = request.ID
				s.publish(agentevents.Event{WorkerID: s.workerID, Kind: agentevents.KindApprovalRequest, At: s.owner.now().UTC(), Approval: &approval})
			}
		case outcome := <-attachResult:
			attaching = false
			if outcome.err != nil {
				if outcome.tries > 1 {
					log.Printf("codex events: %s could not attach to its thread: %v", s.workerID, outcome.err)
				}
				// Try again when the thread next goes active.
				if s.mapper.Active() && outcome.tries == 1 {
					attach(outcome.thread, codexAttachTries)
				}
				continue
			}
			attached[outcome.thread] = true
			s.publish(s.mapper.Notification("thread/started", outcome.threadJSON, s.owner.now().UTC())...)
			if outcome.active {
				s.publish(s.mapper.Notification("thread/status/changed", outcome.statusJSON, s.owner.now().UTC())...)
			}
			s.setThread(s.mapper.Thread())
		case e := <-decided:
			if e.Kind != agentevents.KindApprovalResolved || e.By != "mewla" {
				continue
			}
			id, ok := pending[e.ApprovalID]
			if !ok {
				continue
			}
			delete(pending, e.ApprovalID)
			if response, ok := s.mapper.Decision(e.ApprovalID, e.OptionID); ok {
				if err := client.Respond(id, response); err != nil {
					log.Printf("codex events: %s could not answer approval %s: %v", s.workerID, e.ApprovalID, err)
				}
			}
		}
	}
}

func (s *codexSession) setThread(thread string) {
	s.mu.Lock()
	s.thread = thread
	s.mu.Unlock()
}

// dial connects to the Worker's app-server, which the TUI's launch starts
// moments before the Worker is discovered.
func (s *codexSession) dial(ctx context.Context) *codexctl.Client {
	deadline := time.Now().Add(codexConnectWindow)
	for {
		client, err := codexctl.Open(ctx, s.socket, codexctl.DialOptions{ClientName: "mewla-events", DialTimeout: 3 * time.Second})
		if err == nil {
			return client
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			log.Printf("codex events: %s: no app-server at %s: %v", s.workerID, s.socket, err)
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

type attachOutcome struct {
	thread     string
	tries      int
	threadJSON json.RawMessage
	statusJSON json.RawMessage
	active     bool
	err        error
}

// attach subscribes to the thread once it can be: thread/resume is refused
// until the thread's first turn has written its rollout.
func (s *codexSession) attach(ctx context.Context, client *codexctl.Client, thread string, tries int) attachOutcome {
	var lastErr error
	for try := 0; try < tries; try++ {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, err := client.Attach(callCtx, thread)
		cancel()
		if err == nil {
			var described struct {
				ID     string `json:"id"`
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			}
			_ = json.Unmarshal(raw, &described)
			statusJSON, _ := json.Marshal(map[string]any{"threadId": thread, "status": map[string]any{"type": described.Status.Type}})
			threadJSON, _ := json.Marshal(map[string]any{"thread": json.RawMessage(raw)})
			return attachOutcome{thread: thread, tries: tries, threadJSON: threadJSON, statusJSON: statusJSON, active: described.Status.Type == "active"}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return attachOutcome{thread: thread, tries: tries, err: ctx.Err()}
		case <-time.After(codexAttachGap):
		}
	}
	return attachOutcome{thread: thread, tries: tries, err: lastErr}
}
