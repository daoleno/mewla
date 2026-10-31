package phone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Codes of errors the daemon itself raises, next to the phone's own Code* values.
const (
	CodeNotConnected = "not_connected" // no phone has a connection to this computer
	CodeBusy         = "busy"          // another agent is using the phone
	CodeTimeout      = "timeout"       // the phone did not answer in time
	CodeLost         = "connection_lost"
)

// StateOffline is Status.State while no phone is connected.
const StateOffline = "offline"

const (
	// DefaultTimeout bounds one op the phone answers quickly.
	DefaultTimeout = 30 * time.Second
	// DefaultIdle is how long the phone's display stays with an agent that does nothing.
	DefaultIdle = 60 * time.Second
	// DefaultReconnect is how long an agent's action waits for a phone that dropped to come back
	// (the App reconnects by itself) before it is told the phone is not connected.
	DefaultReconnect = 30 * time.Second
	// HandoffDefault is how long a handoff waits for the person.
	HandoffDefault = 10 * time.Minute
	// handoffGrace lets the phone's own handoff timeout answer before the daemon gives up.
	handoffGrace   = 10 * time.Second
	releaseTimeout = 5 * time.Second
)

// Error is a refusal or failure with a Code an agent, the audit and the tests can tell apart.
// Its message is one or two sentences written for the agent.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// Is matches an *Error with the same Code, so errors.Is(err, ErrStopped) works.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// What an agent is told when the phone cannot be used now.
var (
	ErrNotConnected = &Error{CodeNotConnected, "The phone is not connected to this computer. Tell the person to open Mewla on their phone, and stop."}
	ErrNotReady     = &Error{CodeNotReady, "Phone use isn't ready on the phone: the person has to open Mewla → Phone on their phone. Tell them, and stop."}
	ErrPerson       = &Error{CodePersonInControl, "The person is using the agent's screen on the phone right now. Stop and tell them; do not retry in a loop. It is yours again when they hand it back."}
	ErrStopped      = &Error{CodeStopped, "The person stopped the agent on the phone. Stop and report; do not retry until they hand it back."}
	ErrTimeout      = &Error{CodeTimeout, "The phone did not answer in time. It may be asleep or offline; report this instead of retrying in a loop."}
	ErrLost         = &Error{CodeLost, "The phone disconnected while this was running. Read the screen again once it is back."}
	ErrReplaced     = &Error{CodeLost, "The phone reconnected while this was running. Read the screen again."}
)

// Conn is the daemon's end of the phone's WebSocket. The server writes it as one JSON text frame
// per Send; the manager never calls Send concurrently with itself, but Close may come at any time.
type Conn interface {
	Send(frame []byte) error
	// Close ends the connection with a WebSocket close code and a sentence for the person.
	Close(code int, reason string)
}

// Clock is time as the manager sees it, replaced in tests.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

type Timer interface{ Stop() bool }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// Options tune a Manager; the zero value is what the daemon runs.
type Options struct {
	Clock   Clock
	Timeout time.Duration // an ordinary op; DefaultTimeout
	Idle    time.Duration // DefaultIdle
	// Reconnect is how long an action waits for a missing connection; DefaultReconnect, or a
	// negative value to refuse at once.
	Reconnect time.Duration
}

// Active is the agent using the phone.
type Active struct {
	Worker string    `json:"worker"`
	Name   string    `json:"name,omitempty"`
	Since  time.Time `json:"since"`
}

// Status is what the App shows about the phone.
type Status struct {
	Connected  bool    `json:"connected"`
	State      string  `json:"state"` // the phone's State* value, or StateOffline
	Model      string  `json:"model,omitempty"`
	SDK        int     `json:"sdk,omitempty"`
	AppVersion string  `json:"app_version,omitempty"`
	W          int     `json:"w,omitempty"`
	H          int     `json:"h,omitempty"`
	Active     *Active `json:"active,omitempty"`
}

// Reply is the phone's answer to one op. When the phone answered but refused, Do returns it
// together with the error, because a CodeScreenOff answer still carries a picture.
type Reply struct {
	Result
	// Gen names the connection that answered; ids read from a Screen belong to it.
	Gen uint64
}

// Manager owns the one current phone connection. It sends the phone one request at a time on
// behalf of the agents, refuses actions the phone's state does not allow, remembers who is using
// the phone and gives the phone's display back when the agent goes quiet.
type Manager struct {
	opts Options
	// sem holds the one request in flight.
	sem chan struct{}

	mu   sync.Mutex
	link *Link
	// up is closed, and replaced, when a connection attaches: a caller that finds no phone waits on it.
	up       chan struct{}
	gen      uint64
	nextID   uint64
	active   *Active
	lastAct  time.Time
	inflight int
	idle     Timer
}

// NewManager makes a manager with no phone connected.
func NewManager(opts Options) *Manager {
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Idle <= 0 {
		opts.Idle = DefaultIdle
	}
	if opts.Reconnect == 0 {
		opts.Reconnect = DefaultReconnect
	}
	return &Manager{opts: opts, sem: make(chan struct{}, 1), up: make(chan struct{})}
}

// Link is one phone connection. The server feeds it the phone's frames.
type Link struct {
	m    *Manager
	gen  uint64
	conn Conn
	dead bool

	hello   Hello
	state   string // the phone's last report
	problem string // why a phone in setup could not start its helper, as it said
	stopped bool   // latched by Stop, cleared only by give_back
	person  bool   // the person is driving the agent's screen
	w, h    int

	pending *pending
}

type pending struct {
	id   string
	op   string
	once sync.Once
	done chan outcome
}

type outcome struct {
	result Result
	err    error
}

func (p *pending) finish(o outcome) { p.once.Do(func() { p.done <- o }) }

func knownState(state string) bool {
	switch state {
	case StateReady, StateSetup, StatePersonInCharge, StateStopped:
		return true
	}
	return false
}

// Attach makes conn the current connection, replacing an older one and telling it why. The
// phone's hello must come first and carry a protocol Version this daemon knows; otherwise the
// current connection is left alone.
func (m *Manager) Attach(hello Hello, conn Conn) (*Link, error) {
	if hello.Version != Version {
		return nil, &Error{CodeNotReady, fmt.Sprintf("the phone speaks Mewla phone protocol %d, this computer speaks %d; update the older one", hello.Version, Version)}
	}
	state := hello.State
	if !knownState(state) {
		state = StateSetup
	}
	m.mu.Lock()
	m.gen++
	l := &Link{m: m, gen: m.gen, conn: conn, hello: hello, state: state}
	l.stopped = state == StateStopped
	l.person = state == StatePersonInCharge
	old := m.link
	m.link = l
	close(m.up)
	m.up = make(chan struct{})
	if old != nil {
		old.dead = true
		old.failPending(ErrReplaced)
	}
	m.mu.Unlock()
	if old != nil {
		old.conn.Close(CloseReplaced, "Another connection from your phone took this one's place.")
	}
	return l, nil
}

// Detach tells the manager the connection ended.
func (l *Link) Detach() {
	m := l.m
	m.mu.Lock()
	defer m.mu.Unlock()
	l.dead = true
	l.failPending(ErrLost)
	if m.link == l {
		m.link = nil
		m.active = nil
		m.stopIdleLocked()
	}
}

func (l *Link) failPending(err error) {
	if l.pending != nil {
		l.pending.finish(outcome{err: err})
	}
}

// Receive handles one text frame from the phone: state, event or result. A frame of a type this
// daemon does not know is ignored, so a newer phone can say more than an older daemon hears.
func (l *Link) Receive(frame []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(frame, &head); err != nil {
		return fmt.Errorf("not a JSON frame: %w", err)
	}
	switch head.Type {
	case TypeState:
		var s State
		if err := json.Unmarshal(frame, &s); err != nil {
			return err
		}
		l.m.onState(l, s)
	case TypeEvent:
		var e Event
		if err := json.Unmarshal(frame, &e); err != nil {
			return err
		}
		l.m.onEvent(l, e)
	case TypeResult:
		var r Result
		if err := json.Unmarshal(frame, &r); err != nil {
			return err
		}
		l.m.onResult(l, r)
	}
	return nil
}

func (m *Manager) onState(l *Link, s State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.dead {
		return
	}
	if !knownState(s.State) {
		s.State = StateSetup
	}
	if l.state != s.State {
		log.Printf("phone: state %s -> %s%s", l.state, s.State, problemSuffix(trimProblem(s.Problem)))
	}
	l.state = s.State
	l.problem = trimProblem(s.Problem)
	// The phone says whose hands the agent's screen is in; a stop it reports is latched until
	// give_back, like one the person pressed.
	l.person = s.State == StatePersonInCharge
	if s.State == StateStopped {
		l.stopped = true
	}
	if s.W > 0 && s.H > 0 {
		l.w, l.h = s.W, s.H
	}
	if s.State == StatePersonInCharge && l.pending != nil && l.pending.op == OpHandoff {
		// A handoff is the phone putting the person in charge: it is what the request asked for,
		// not a reason to fail it. Stop, take-over and setup below still end it.
		return
	}
	if s.State != StateReady {
		l.failPending(l.refusalLocked())
	}
}

func (m *Manager) onEvent(l *Link, e Event) {
	m.mu.Lock()
	if l.dead {
		m.mu.Unlock()
		return
	}
	switch e.Kind {
	case EventStop:
		m.stopLocked(l)
		m.mu.Unlock()
		m.freeDisplay()
		return
	case EventTakeOver:
		l.person = true
		l.failPending(ErrPerson)
		m.active = nil
		m.stopIdleLocked()
	case EventGiveBack:
		l.stopped, l.person = false, false
		if l.state == StateStopped || l.state == StatePersonInCharge {
			l.state = StateReady
		}
	case EventHandedOff:
		// A handoff is answered by this event as well as by a Result.
		if p := l.pending; p != nil && p.op == OpHandoff && (e.ID == "" || e.ID == p.id) {
			p.finish(outcome{result: Result{Type: TypeResult, ID: p.id, OK: true, Note: "The person says they are done."}})
			// Done hands the screen back. The phone says so in a state frame of its own, but only
			// after this event, and the agent's next look at the screen must not wait for that.
			l.person = false
			if l.state == StatePersonInCharge {
				l.state = StateReady
			}
		}
	}
	m.mu.Unlock()
}

func (m *Manager) onResult(l *Link, r Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.dead {
		return
	}
	p := l.pending
	if p == nil || p.id != r.ID {
		return // late: its request already ended
	}
	if r.Screen != nil && r.Screen.W > 0 && r.Screen.H > 0 {
		l.w, l.h = r.Screen.W, r.Screen.H
	}
	if r.OK {
		p.finish(outcome{result: r})
		return
	}
	p.finish(outcome{result: r, err: resultError(r)})
}

func resultError(r Result) *Error {
	code := r.Code
	if code == "" {
		code = CodeFailed
	}
	msg := r.Error
	if msg == "" {
		switch code {
		case CodePersonInControl:
			return ErrPerson
		case CodeStopped:
			return ErrStopped
		case CodeNotReady:
			return ErrNotReady
		case CodeNotInstalled:
			msg = "That app is not installed on the phone."
		case CodeInUse:
			msg = "The person is using that app on the phone's own screen right now. Try another app, or ask them."
		case CodeScreenOff:
			msg = "The phone's own screen is off, so the screen cannot be read."
		default:
			msg = "The phone could not do that."
		}
	}
	switch code {
	case CodePersonInControl, CodeStopped:
		// The phone's words, plus the rule that matters most.
		msg += " Stop and report; do not retry in a loop."
	}
	return &Error{code, msg}
}

// refusalLocked says why no action may be sent now, or nil.
func (l *Link) refusalLocked() error {
	switch {
	case l.stopped:
		return ErrStopped
	case l.person:
		return ErrPerson
	case l.state != StateReady:
		if l.problem != "" {
			return &Error{CodeNotReady, strings.TrimSuffix(ErrNotReady.Message, " Tell them, and stop.") + " (" + l.problem + "). Tell them, and stop."}
		}
		return ErrNotReady
	}
	return nil
}

// trimProblem keeps the phone's reason to a sentence: it goes into an agent's error.
func trimProblem(problem string) string {
	problem = strings.TrimSpace(problem)
	if r := []rune(problem); len(r) > 160 {
		return string(r[:160])
	}
	return problem
}

func problemSuffix(problem string) string {
	if problem == "" {
		return ""
	}
	return " (" + problem + ")"
}

func (l *Link) effectiveLocked() string {
	switch {
	case l.stopped:
		return StateStopped
	case l.person:
		return StatePersonInCharge
	}
	return l.state
}

// stopLocked ends what the agent is doing and refuses actions until the person gives it back.
func (m *Manager) stopLocked(l *Link) {
	l.stopped = true
	l.failPending(ErrStopped)
	m.active = nil
	m.stopIdleLocked()
}

// Stop is the person's Stop, from the App: the in-flight request ends, the agent is no longer
// active, and actions are refused until the phone reports give_back.
func (m *Manager) Stop() {
	m.mu.Lock()
	l := m.link
	if l == nil {
		m.active = nil
		m.mu.Unlock()
		return
	}
	m.stopLocked(l)
	m.mu.Unlock()
	m.freeDisplay()
}

// Release ends the agent's use of the phone without blocking it from the next one: Phone use
// turned off.
func (m *Manager) Release() {
	m.mu.Lock()
	if l := m.link; l != nil {
		l.failPending(&Error{CodeStopped, "Phone use was turned off. Stop and report."})
	}
	m.active = nil
	m.stopIdleLocked()
	m.mu.Unlock()
	m.freeDisplay()
}

// freeDisplay tells the phone the agent is done, without making the caller wait.
func (m *Manager) freeDisplay() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout+time.Second)
		defer cancel()
		_, _ = m.Do(ctx, "", "", OpRelease, nil, false)
	}()
}

// awaitLink gives a phone that dropped a moment to come back: the App reconnects by itself, so an
// agent's action waits up to the reconnect window instead of failing at once. It returns when a
// phone is connected, the window ends or ctx does; callers then run their gates as usual.
func (m *Manager) awaitLink(ctx context.Context) {
	m.mu.Lock()
	if m.link != nil || m.opts.Reconnect < 0 {
		m.mu.Unlock()
		return
	}
	up := m.up
	m.mu.Unlock()
	timer := time.NewTimer(m.opts.Reconnect)
	defer timer.Stop()
	select {
	case <-up:
	case <-timer.C:
	case <-ctx.Done():
	}
}

// CheckWait is Check for an agent about to use the phone: a missing connection gets the reconnect window first.
func (m *Manager) CheckWait(ctx context.Context) error {
	m.awaitLink(ctx)
	return m.Check()
}

// Check says why an action could not be sent now, or nil.
func (m *Manager) Check() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.link == nil {
		return ErrNotConnected
	}
	return m.link.refusalLocked()
}

// Status is the phone as the App shows it.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{State: StateOffline}
	if l := m.link; l != nil {
		status = Status{Connected: true, State: l.effectiveLocked(), Model: l.hello.Model, SDK: l.hello.SDK, AppVersion: l.hello.AppVersion, W: l.w, H: l.h}
	}
	if m.active != nil {
		active := *m.active
		status.Active = &active
	}
	return status
}

// Generation numbers the current connection (0 while none): ids read on one connection mean
// nothing on the next.
func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.link == nil {
		return 0
	}
	return m.link.gen
}

// Do asks the phone to do one op for an agent and waits for the answer. Requests are sent one at
// a time; a caller waits its turn. The agent is refused, with an *Error, while the phone is not
// connected or ready, the person is in control or has stopped it, or another agent is using it.
// An empty worker is the daemon itself (releasing the display).
func (m *Manager) Do(ctx context.Context, worker, workerName, op string, args any, image bool) (Reply, error) {
	var raw json.RawMessage
	switch v := args.(type) {
	case nil:
	case json.RawMessage:
		raw = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return Reply{}, err
		}
		raw = b
	}
	// An agent that cannot have the phone is told so at once, not after waiting its turn behind
	// another's handoff; the same gates run again once it has the turn.
	if worker != "" {
		m.awaitLink(ctx)
		m.mu.Lock()
		err := m.gateLocked(worker)
		m.mu.Unlock()
		if err != nil {
			return Reply{}, err
		}
	}
	select {
	case m.sem <- struct{}{}:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Reply{}, ErrTimeout
		}
		return Reply{}, ctx.Err()
	}
	defer func() { <-m.sem }()

	if worker != "" {
		// The phone may have dropped while this caller waited its turn.
		m.awaitLink(ctx)
	}
	m.mu.Lock()
	l := m.link
	if l == nil {
		m.mu.Unlock()
		if worker == "" {
			return Reply{}, nil
		}
		return Reply{}, ErrNotConnected
	}
	if worker == "" {
		// The daemon's own release: the person's screen is never taken from them.
		if l.person || l.state == StateSetup {
			m.mu.Unlock()
			return Reply{}, nil
		}
	} else {
		if err := m.gateLocked(worker); err != nil {
			m.mu.Unlock()
			return Reply{}, err
		}
		if m.active == nil {
			m.active = &Active{Worker: worker, Name: workerName, Since: m.opts.Clock.Now()}
		}
		if workerName != "" {
			m.active.Name = workerName
		}
		m.inflight++
		m.stopIdleLocked()
	}
	m.nextID++
	p := &pending{id: "r" + strconv.FormatUint(m.nextID, 10), op: op, done: make(chan outcome, 1)}
	l.pending = p
	m.mu.Unlock()

	req := Request{Type: TypeRequest, ID: p.id, Op: op, Worker: worker, WorkerName: workerName, Args: raw, Image: image}
	frame, _ := json.Marshal(req)
	var o outcome
	if err := l.conn.Send(frame); err != nil {
		o = outcome{err: &Error{CodeLost, "The phone connection failed: " + err.Error()}}
	} else {
		timer := time.NewTimer(m.timeoutFor(op, raw))
		select {
		case o = <-p.done:
		case <-ctx.Done():
			o = outcome{err: ctx.Err()}
			if errors.Is(o.err, context.DeadlineExceeded) {
				o.err = ErrTimeout
			}
		case <-timer.C:
			o = outcome{err: ErrTimeout}
		}
		timer.Stop()
	}

	m.mu.Lock()
	if l.pending == p {
		l.pending = nil
	}
	if worker != "" {
		m.inflight--
		m.lastAct = m.opts.Clock.Now()
		m.armIdleLocked()
	}
	m.mu.Unlock()
	if errors.Is(o.err, ErrTimeout) && op != OpRelease {
		// A phone stuck on this op may still be holding the agent's display.
		m.freeDisplay()
	}
	return Reply{Result: o.result, Gen: l.gen}, o.err
}

// gateLocked says why worker may not use the phone now, or nil: not connected, not ready, the
// person in control or having stopped it, or another agent using it.
func (m *Manager) gateLocked(worker string) error {
	l := m.link
	if l == nil {
		return ErrNotConnected
	}
	if err := l.refusalLocked(); err != nil {
		return err
	}
	if m.active != nil && m.active.Worker != worker {
		name := m.active.Name
		if name == "" {
			name = m.active.Worker
		}
		return &Error{CodeBusy, "Another agent (" + name + ") is using the phone. Stop and report; it is free a minute after that agent goes quiet."}
	}
	return nil
}

func (m *Manager) timeoutFor(op string, raw json.RawMessage) time.Duration {
	switch op {
	case OpHandoff:
		var a HandoffArgs
		_ = json.Unmarshal(raw, &a)
		d := time.Duration(a.TimeoutMS) * time.Millisecond
		if d <= 0 {
			d = HandoffDefault
		}
		return d + handoffGrace
	case OpWait:
		var a WaitArgs
		_ = json.Unmarshal(raw, &a)
		return time.Duration(a.MS)*time.Millisecond + m.opts.Timeout
	case OpRelease:
		return releaseTimeout
	}
	return m.opts.Timeout
}

func (m *Manager) stopIdleLocked() {
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
}

// armIdleLocked starts the countdown to giving the display back.
func (m *Manager) armIdleLocked() {
	m.stopIdleLocked()
	if m.active == nil || m.inflight > 0 {
		return
	}
	m.idle = m.opts.Clock.AfterFunc(m.opts.Idle, m.idleFired)
}

func (m *Manager) idleFired() {
	m.mu.Lock()
	if m.active == nil || m.inflight > 0 || m.opts.Clock.Now().Sub(m.lastAct) < m.opts.Idle {
		m.mu.Unlock()
		return
	}
	m.active = nil
	m.idle = nil
	m.mu.Unlock()
	m.freeDisplay()
}
