package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// agyEvents is the daemon end of the Antigravity Session program
// (work/antigravity_session.go). Each Session connects to one local socket,
// names itself, and from then on reports agy's lifecycle (turns, messages,
// tool calls, the decisions agy waits on) and takes messages, stops, answers
// and model changes back. A Session outlives a daemon restart; it connects
// again and restates what is still true.
type agyEvents struct {
	ctx             context.Context
	hub             *agentevents.Hub
	service         *agentEventService
	now             func() time.Time
	workers         func() []*classifier.Worker
	ackWait         time.Duration
	providerOutcome func(watcher.ProviderActivityObservation)

	mu    sync.Mutex
	conns map[string]*agyConn
}

type agyConn struct {
	owner    *agyEvents
	conn     net.Conn
	workerID string
	session  string

	writeMu sync.Mutex
	mu      sync.Mutex
	acks    map[string]chan error
	replies map[string]chan work.AntigravityLinkMessage
	// asking holds the decisions this Session waits on, by id, with the
	// cancel of the wait on a person or Brain.
	asking map[string]context.CancelFunc
	// questions are the questions agy asked with its ask_question tool. agy
	// ends its turn while one waits (the answer is the next message), and the
	// hub settles a decision whenever the agent moves, so these are stated
	// again after every event that would settle them.
	questions    map[string]agentevents.Approval
	conversation string
}

func newAgyEvents(ctx context.Context, hub *agentevents.Hub, service *agentEventService, workers func() []*classifier.Worker) *agyEvents {
	return &agyEvents{ctx: ctx, hub: hub, service: service, now: time.Now, workers: workers, ackWait: 5 * time.Second, conns: map[string]*agyConn{}}
}

// Listen accepts Session connections on a socket only this user can open.
func (a *agyEvents) Listen(path string) error {
	if err := platform.EnsureSocketDir(path); err != nil {
		return err
	}
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
		<-a.ctx.Done()
		listener.Close()
		_ = os.Remove(path)
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go a.serve(conn)
		}
	}()
	return nil
}

// workerFor is the Worker whose command runs Session id.
func (a *agyEvents) workerFor(session string) string {
	if session == "" {
		return ""
	}
	for _, w := range a.workers() {
		if w != nil && work.AntigravitySessionID(w.Command) == session {
			return w.ID
		}
	}
	return ""
}

func (a *agyEvents) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, 1<<20)
	var c *agyConn
	defer func() {
		if c != nil {
			a.drop(c)
		}
	}()
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg work.AntigravityLinkMessage
		if json.Unmarshal(line, &msg) != nil || msg.T == "" {
			continue
		}
		at := a.now().UTC()
		if msg.T == "hello" {
			if c == nil {
				workerID := ""
				// The watcher may not have seen the new Session yet.
				for attempt := 0; attempt < 20 && workerID == ""; attempt++ {
					if workerID = a.workerFor(msg.Session); workerID == "" {
						time.Sleep(250 * time.Millisecond)
					}
				}
				if workerID == "" {
					return
				}
				c = &agyConn{owner: a, conn: conn, workerID: workerID, session: msg.Session, acks: map[string]chan error{}, replies: map[string]chan work.AntigravityLinkMessage{}, asking: map[string]context.CancelFunc{}, questions: map[string]agentevents.Approval{}}
				a.attach(c)
			}
			c.conversation = msg.Conversation
			if msg.Conversation != "" {
				binding := agentevents.Binding{ConversationID: msg.Conversation, Ref: msg.Log, By: "launch"}
				a.hub.Publish(agentevents.Event{WorkerID: c.workerID, Kind: agentevents.KindBound, At: at, Binding: &binding})
			}
			if msg.Running && msg.Turn != "" {
				a.hub.Publish(agentevents.Event{WorkerID: c.workerID, Kind: agentevents.KindTurnStarted, At: at, TurnID: msg.Turn, Status: agentevents.TurnRunning})
			}
			for _, ask := range msg.Asks {
				c.ask(ask)
			}
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
		case "reply":
			c.reply(msg)
		case "ask":
			if msg.Ask != nil {
				c.ask(*msg.Ask)
			}
		case "resolved":
			c.resolved(msg.ID, msg.By)
		case "bye":
			a.hub.Publish(agentevents.Event{WorkerID: c.workerID, Kind: agentevents.KindExit, At: at})
		default:
			events := agyEventsOf(c.workerID, msg, at)
			for _, e := range events {
				a.hub.Publish(e)
			}
			if msg.T == "turn_end" && a.providerOutcome != nil {
				a.providerOutcome(agyProviderOutcome(msg, at))
			}
			if len(events) > 0 {
				c.restateQuestions(at)
			}
		}
	}
}

func agyProviderOutcome(msg work.AntigravityLinkMessage, at time.Time) watcher.ProviderActivityObservation {
	observation := watcher.ProviderActivityObservation{ID: msg.Turn, ProbeState: watcher.ProbeStateOK, SettledAt: at}
	switch msg.Stop {
	case "end_turn", "max_tokens", "max_turn_requests":
		observation.Status = string(work.ProviderActivityCompleted)
	case "cancelled":
		observation.Status = string(work.ProviderActivityInterrupted)
	default:
		observation.Status = string(work.ProviderActivityFailed)
		observation.Error = &watcher.ProviderActivityError{Code: firstNonEmptyString(msg.ErrorCode, msg.Stop), HTTPStatus: msg.HTTPStatus, Message: msg.Error}
	}
	return observation
}

// agyEventsOf maps one Session message to the events it carries.
func agyEventsOf(workerID string, msg work.AntigravityLinkMessage, at time.Time) []agentevents.Event {
	ev := func(kind agentevents.EventKind) agentevents.Event {
		return agentevents.Event{WorkerID: workerID, Kind: kind, At: at, TurnID: msg.Turn}
	}
	switch msg.T {
	case "input":
		if msg.SHA == "" {
			return nil
		}
		e := ev(agentevents.KindUserMessage)
		e.TurnID = ""
		e.ID = "agy-input-" + firstNonEmptyString(msg.ID, fmt.Sprint(at.UnixNano()))
		e.ClientID = msg.ID
		e.InputSHA256, e.InputUnwrappedSHA256 = msg.SHA, msg.SHA
		return []agentevents.Event{e}
	case "turn_start":
		e := ev(agentevents.KindTurnStarted)
		e.Status = agentevents.TurnRunning
		return []agentevents.Event{e}
	case "activity":
		e := ev(agentevents.KindActivity)
		e.ActivityTool, e.ActivitySummary = msg.Tool, msg.Summary
		e.Activity = strings.TrimSpace(msg.Summary)
		return []agentevents.Event{e}
	case "progress":
		return []agentevents.Event{ev(agentevents.KindProgress)}
	case "turn_end":
		e := ev(agentevents.KindTurnFinished)
		switch msg.Stop {
		case "end_turn", "max_tokens", "max_turn_requests":
			e.Status = agentevents.TurnCompleted
		case "cancelled":
			e.Status = agentevents.TurnInterrupted
		default:
			e.Status = agentevents.TurnFailed
			e.Error = &agentevents.TurnError{Class: agentevents.ErrorUnknown, Code: firstNonEmptyString(msg.ErrorCode, msg.Stop), HTTPStatus: msg.HTTPStatus, Message: strings.TrimSpace(msg.Error)}
		}
		return []agentevents.Event{e}
	}
	return nil
}

func (a *agyEvents) attach(c *agyConn) {
	a.mu.Lock()
	old := a.conns[c.workerID]
	a.conns[c.workerID] = c
	a.mu.Unlock()
	if old != nil {
		_ = old.conn.Close()
	}
	a.hub.SetConnected(c.workerID, true)
}

func (a *agyEvents) drop(c *agyConn) {
	a.mu.Lock()
	current := a.conns[c.workerID] == c
	if current {
		delete(a.conns, c.workerID)
	}
	a.mu.Unlock()
	c.mu.Lock()
	asking := c.asking
	c.asking = map[string]context.CancelFunc{}
	c.mu.Unlock()
	for _, cancel := range asking {
		cancel()
	}
	if current {
		a.hub.SetConnected(c.workerID, false)
	}
}

func (a *agyEvents) conn(workerID string) *agyConn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[workerID]
}

func (c *agyConn) write(v any) error {
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

func (c *agyConn) ack(id string, err error) {
	c.mu.Lock()
	ch := c.acks[id]
	delete(c.acks, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- err
	}
}

func (c *agyConn) reply(msg work.AntigravityLinkMessage) {
	c.mu.Lock()
	ch := c.replies[msg.Req]
	delete(c.replies, msg.Req)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

// request sends one request and waits for its reply.
func (c *agyConn) request(ctx context.Context, message map[string]any) (work.AntigravityLinkMessage, error) {
	req := uuid.NewString()
	message["req"] = req
	ch := make(chan work.AntigravityLinkMessage, 1)
	c.mu.Lock()
	c.replies[req] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.replies, req)
		c.mu.Unlock()
	}()
	if err := c.write(message); err != nil {
		return work.AntigravityLinkMessage{}, err
	}
	select {
	case reply := <-ch:
		if !reply.OK && reply.Error != "" {
			return reply, errors.New(reply.Error)
		}
		return reply, nil
	case <-ctx.Done():
		return work.AntigravityLinkMessage{}, ctx.Err()
	}
}

// ask hands a decision agy waits on to the permission gate: Brain first for a
// delegated Worker, otherwise the person's approval card. The answer goes back
// to the Session. A permission nobody answered for a delegated Worker is
// refused with the reason, so agy is never left waiting.
func (c *agyConn) ask(ask work.AntigravityAsk) {
	c.mu.Lock()
	if _, known := c.asking[ask.ID]; known {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(c.owner.ctx)
	c.asking[ask.ID] = cancel
	c.mu.Unlock()
	approval := agentevents.Approval{ID: ask.ID, ConversationID: c.conversation, Kind: ask.Kind, Tool: ask.Tool, Summary: ask.Summary}
	for _, option := range ask.Options {
		approval.Options = append(approval.Options, agentevents.ApprovalOption{ID: option.ID, Label: option.Label, Scope: option.Scope})
	}
	if ask.Kind == "question" {
		c.mu.Lock()
		c.questions[ask.ID] = approval
		c.mu.Unlock()
	}
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.asking, ask.ID)
			delete(c.questions, ask.ID)
			c.mu.Unlock()
			cancel()
		}()
		decision, answered, delegated := c.owner.service.ask(ctx, c.workerID, approval)
		if !answered {
			if ctx.Err() == nil && delegated && ask.Kind == "permission" && !c.owner.service.stopping() {
				_ = c.write(map[string]any{"t": "decide", "id": ask.ID, "option": agyDenyOption(ask), "by": "timeout"})
			}
			return
		}
		_ = c.write(map[string]any{"t": "decide", "id": ask.ID, "option": decision.OptionID, "by": "mewla"})
	}()
}

func agyDenyOption(ask work.AntigravityAsk) string {
	for _, option := range ask.Options {
		if option.Scope == "deny" {
			return option.ID
		}
	}
	return "deny"
}

// restateQuestions puts the questions agy still waits on back on the hub
// after an event that settled them there (see questions).
func (c *agyConn) restateQuestions(at time.Time) {
	c.mu.Lock()
	var pending []agentevents.Approval
	for _, approval := range c.questions {
		pending = append(pending, approval)
	}
	c.mu.Unlock()
	state, _ := c.owner.hub.State(c.workerID)
	for _, approval := range pending {
		listed := false
		for _, have := range state.Approvals {
			listed = listed || have.ID == approval.ID
		}
		if !listed {
			copied := approval
			c.owner.hub.Publish(agentevents.Event{WorkerID: c.workerID, Kind: agentevents.KindApprovalRequest, At: at, Approval: &copied})
		}
	}
}

// resolved is a decision the Session settled itself (answered in its
// terminal, by a message, withdrawn when the turn or agy ended).
func (c *agyConn) resolved(id, by string) {
	c.mu.Lock()
	cancel := c.asking[id]
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	if by == "mewla" || by == "timeout" {
		return
	}
	c.owner.hub.AnsweredInTerminal(c.workerID, id, c.owner.now().UTC())
	cancel()
}

// send hands a message to agy through its Session. handled is false for a
// Worker with no connected Session, which is typed into its terminal instead.
func (a *agyEvents) send(sessionID, payload, receipt string, begin func() error) (handled bool, err error) {
	c := a.conn(sessionID)
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
			return true, &watcher.StructuredInputError{Err: err, NotSent: true}
		}
		return true, nil
	case <-time.After(a.ackWait):
		return true, &watcher.StructuredInputError{Err: errors.New("the Antigravity Session did not acknowledge the message")}
	case <-a.ctx.Done():
		return true, a.ctx.Err()
	}
}

// interrupt stops the Worker's running turn (ACP session/cancel).
func (a *agyEvents) interrupt(sessionID string) (handled bool, err error) {
	c := a.conn(sessionID)
	if c == nil {
		return false, nil
	}
	return true, c.write(map[string]string{"t": "abort"})
}

// Linked reports a Session whose program is connected to Mewla.
func (a *agyEvents) Linked(workerID string) bool { return a.conn(workerID) != nil }

// Models asks a Session what agy offers and uses now.
func (a *agyEvents) Models(ctx context.Context, workerID string) (json.RawMessage, error) {
	c := a.conn(workerID)
	if c == nil {
		return nil, errAgySessionAway
	}
	reply, err := c.request(ctx, map[string]any{"t": "models"})
	return reply.Models, err
}

// SetModel changes the model, thinking level or mode of a Session.
func (a *agyEvents) SetModel(ctx context.Context, workerID, model, effort, mode string) (json.RawMessage, error) {
	c := a.conn(workerID)
	if c == nil {
		return nil, errAgySessionAway
	}
	reply, err := c.request(ctx, map[string]any{"t": "set_model", "model": model, "effort": effort, "mode": mode})
	return reply.Models, err
}

var errAgySessionAway = errors.New("the Antigravity Session is not connected to Mewla")

// Observe lets go of a Worker that is gone.
func (a *agyEvents) Observe(event watcher.SessionEvent) {
	if event.Type != "worker_removed" {
		return
	}
	if c := a.conn(event.WorkerID); c != nil {
		_ = c.conn.Close()
	}
}

func logAgyListen(err error) {
	if err != nil {
		log.Printf("WARN agent events: cannot listen for Antigravity Sessions, they are read from their log only: %v", err)
	}
}
