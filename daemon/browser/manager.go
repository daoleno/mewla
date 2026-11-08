package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const LeaseLifetime = 30 * time.Second

// leaseLifetime is LeaseLifetime; a variable so tests can shorten it.
var leaseLifetime = LeaseLifetime

var validID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

type entry struct {
	transition   sync.Mutex
	deleted      bool
	mu           sync.Mutex
	gate         chan struct{}
	resource     Resource
	runtime      Runtime
	key          string // the extension's id for the window
	owner        Owner  // the person, while they hold the window
	epoch        uint64
	expires      time.Time
	expiry       *time.Timer // fires when expires passes; see armExpiryLocked
	transferring bool
	viewer       bool

	// What the person did in a window an agent works in, told to the agent
	// with its next result; see touch.go.
	touches    []touch
	agentBusy  int             // tool calls of the agent that are running
	halted     map[string]bool // Workers the person stopped
	humanFrom  time.Time       // when the person took the lease
	handedBack *span           // the person's last turn, not yet told to the agent
}

// Lock order: m.mu guards only the two maps and workerLive, and is never held
// while an entry's lock is taken, nor taken while one is held. Everything an
// entry's code needs from the manager (the switch, whether it is closed) is
// atomic for that reason; a deadlock here once froze every agent at once.
type Manager struct {
	mu          sync.Mutex
	root        string
	backend     Backend
	entries     map[string]*entry // agents' windows, by row id
	byWorker    map[string]*entry // the same, by the Worker whose window it is
	byKey       map[string]*entry // the same, by the extension's id for the window
	closeWindow func(key string)
	switchHook  func()
	closed      atomic.Bool
	done        chan struct{}

	virtual    atomic.Bool // Settings.VirtualScreen
	settingsMu sync.Mutex  // serializes writes of settings.json

	// The computer's one browser. Starting and stopping it, and opening a
	// window on it, go through hostGate one at a time, so an agent that asks
	// while another's start is under way waits for it instead of failing.
	hostGate   chan struct{}
	host       Host
	windows    atomic.Int32
	ensuredAt  atomic.Int64 // unix nanoseconds of the last EnsureHost
	workerLive func(worker string) bool
}

// agentProfile is the one profile agents browse with, under the browsers directory.
const agentProfile = "agent"

func privateDir(path string) error {
	clean, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for at := clean; ; at = filepath.Dir(at) {
		info, e := os.Lstat(at)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Browser directory must not be a symlink")
		}
		if at == filepath.Dir(at) {
			break
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("Browser directory must not be a symlink")
	}
	return os.Chmod(path, 0700)
}
func New(root string, backend Backend) (*Manager, error) {
	if err := privateDir(root); err != nil {
		return nil, err
	}
	if err := adoptOldProfile(root); err != nil {
		return nil, err
	}
	m := &Manager{root: root, backend: backend, entries: map[string]*entry{}, byWorker: map[string]*entry{}, byKey: map[string]*entry{}, done: make(chan struct{}), hostGate: make(chan struct{}, 1)}
	m.hostGate <- struct{}{}
	m.virtual.Store(loadSettings(root).VirtualScreen)
	go m.sweep(sweepEvery)
	return m, nil
}

// adoptOldProfile turns the oldest profile the person made when browsers were
// named and chosen into the agent profile, keeping its sign-ins, and removes
// the others. It runs until there is nothing left to adopt.
func adoptOldProfile(root string) error {
	files, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	type old struct {
		name    string
		created time.Time
	}
	var olds []old
	for _, f := range files {
		if !validID.MatchString(f.Name()) {
			continue
		}
		if f.Type()&os.ModeSymlink != 0 || !f.IsDir() {
			return errors.New("Invalid browser resource directory")
		}
		var r Resource
		if raw, err := os.ReadFile(filepath.Join(root, f.Name(), "resource.json")); err == nil {
			_ = json.Unmarshal(raw, &r)
		}
		olds = append(olds, old{f.Name(), r.CreatedAt})
	}
	if len(olds) == 0 {
		return nil
	}
	sort.Slice(olds, func(i, j int) bool { return olds[i].created.Before(olds[j].created) })
	agent := filepath.Join(root, agentProfile)
	if _, err := os.Lstat(agent); os.IsNotExist(err) {
		first := olds[0]
		olds = olds[1:]
		if err := os.Rename(filepath.Join(root, first.name), agent); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(agent, "resource.json"))
		log.Printf("browser: %s is now the agent profile", first.name)
	} else if err != nil {
		return err
	}
	for _, o := range olds {
		if err := os.RemoveAll(filepath.Join(root, o.name)); err != nil {
			return err
		}
	}
	return nil
}
func newEntry(r Resource) *entry {
	e := &entry{resource: r, gate: make(chan struct{}, 1)}
	e.gate <- struct{}{}
	return e
}
func (m *Manager) get(id string) (*entry, error) {
	if !validID.MatchString(id) {
		return nil, errors.New("Invalid browser resource")
	}
	if m.closed.Load() {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	if e == nil {
		return nil, errors.New("Browser not found")
	}
	return e, nil
}
func snapshot(e *entry) Resource {
	r := e.resource
	if e.runtime != nil {
		if !e.runtime.Alive() {
			r.State = "stopped"
		}
		r.Target = e.runtime.Target()
	}
	// The agent works in the window unless the person holds it.
	switch {
	case e.transferring:
		r.Control = "quiescing"
	case e.owner.ID != "":
		r.Control = e.owner.Kind
	default:
		r.Control = "agent"
	}
	return r
}
func acquire(ctx context.Context, e *entry) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.gate:
		return nil
	}
}
func unlock(e *entry) { e.gate <- struct{}{} }

// Handle is the person's side of a window (list, status, control, release,
// touch, command, halt, and the switch) and the agent's two calls around each
// of its browser tool calls: begin, which the person may refuse, and end,
// which tells what the person did meanwhile.
func (m *Manager) Handle(ctx context.Context, o Owner, q Request) (Response, error) {
	if (o.Kind != "human" && o.Kind != "agent") || o.ID == "" {
		return Response{}, errors.New("Browser owner is required")
	}
	if o.Kind == "agent" {
		return m.agentCall(ctx, o, q)
	}
	if q.Action == "list" {
		resources := []Resource{}
		for _, e := range m.allEntries() {
			e.mu.Lock()
			resources = append(resources, snapshot(e))
			e.mu.Unlock()
		}
		sort.Slice(resources, func(i, j int) bool { return resources[i].CreatedAt.Before(resources[j].CreatedAt) })
		c := m.backend.Capability(ctx)
		on := m.VirtualScreen()
		return Response{Resources: resources, Capability: &c, VirtualScreen: &on}, nil
	}
	if q.Action == "settings" {
		if q.Enabled != nil {
			if err := m.setVirtualScreen(*q.Enabled); err != nil {
				return Response{}, err
			}
		}
		on := m.VirtualScreen()
		return Response{VirtualScreen: &on}, nil
	}
	e, err := m.get(q.ID)
	if err != nil {
		return Response{}, err
	}
	// Fence lifecycle transitions separately from in-flight commands.
	if q.Action != "command" && q.Action != "touch" {
		e.transition.Lock()
		defer e.transition.Unlock()
	}
	e.mu.Lock()
	deleted := e.deleted
	e.mu.Unlock()
	if deleted {
		return Response{}, errors.New("Browser not found")
	}
	switch q.Action {
	case "status":
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "control":
		return m.control(ctx, e, o)
	case "release":
		err = m.releaseMatching(ctx, e, func(x Owner) bool { return x == o })
		return Response{}, err
	case "touch":
		return m.touch(ctx, e, o, q)
	case "halt":
		if err = m.halt(ctx, e); err != nil {
			return Response{}, err
		}
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "command":
		if q.Command == nil || q.Lease == nil {
			return Response{}, ErrLease
		}
		return m.command(ctx, e, o, *q.Lease, *q.Command)
	default:
		return Response{}, errors.New("Unknown browser operation")
	}
}

// agentCall is begin and end around one browser tool call of a Worker. A
// Worker with no window yet is not refused: its first tool call opens one.
func (m *Manager) agentCall(ctx context.Context, o Owner, q Request) (Response, error) {
	worker := workerOf(o)
	m.mu.Lock()
	e := m.byWorker[worker]
	m.mu.Unlock()
	switch q.Action {
	case "begin":
		if e == nil {
			return Response{}, nil
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.deleted {
			return Response{}, nil
		}
		if e.halted[worker] {
			return Response{}, errHalted
		}
		if e.transferring || e.owner.Kind == "human" {
			return Response{}, errPersonInControl
		}
		e.agentBusy++
		return Response{}, nil
	case "end":
		if e == nil {
			return Response{}, nil
		}
		e.mu.Lock()
		if e.agentBusy > 0 {
			e.agentBusy--
		}
		e.mu.Unlock()
		return Response{Notes: m.notesFor(ctx, e)}, nil
	}
	return Response{}, errors.New("Agents may only begin and end a browser call")
}

// allEntries is a copy of the entries, so a caller can lock each without holding m.mu.
func (m *Manager) allEntries() []*entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	return entries
}

// sweepEvery is how often the windows of ended Workers are looked for; a variable so tests can shorten it.
var sweepEvery = 10 * time.Second

// maxWindows is the most agent windows one computer keeps.
const maxWindows = 8

// drainWait is how long a take over waits for the agent's running tool calls
// before it goes ahead anyway: a call cannot be cancelled from here.
var drainWait = 10 * time.Second

// hostGrace is how long a browser that was just asked for may stand with no
// window before it is stopped as idle: its first window is still opening.
var hostGrace = 2 * time.Minute

// SetWindowCloser tells the manager how to close a window in the browser, by
// the extension's id for it, when its Worker is gone.
func (m *Manager) SetWindowCloser(f func(key string)) {
	m.mu.Lock()
	m.closeWindow = f
	m.mu.Unlock()
}

// EnsureHost starts the agent browser (a private display, Chrome and the
// extension) when it is not running, and waits for it. Agents that ask while
// it is starting wait for the same start.
func (m *Manager) EnsureHost(ctx context.Context) error {
	m.ensuredAt.Store(time.Now().UnixNano())
	err := m.withHost(ctx, func(Host) error { return nil })
	if err == nil {
		time.AfterFunc(hostGrace+time.Second, func() { m.stopIdleHost() })
	}
	return err
}

// Opened registers the window the extension opened for a Worker, so the Screen
// lists it. key is the extension's id for the window and target the page it
// shows. A Worker has one row; a window it opens again replaces the old one.
func (m *Manager) Opened(ctx context.Context, worker, key, target string) error {
	rowKey := worker
	if rowKey == "" {
		rowKey = "window:" + key
	}
	m.mu.Lock()
	if m.closed.Load() {
		m.mu.Unlock()
		return ErrUnavailable
	}
	e := m.byWorker[rowKey]
	if e == nil {
		if len(m.entries) >= maxWindows {
			m.mu.Unlock()
			return fmt.Errorf("%w: %d agents already have windows; wait for one to finish", ErrBusy, maxWindows)
		}
		e = newEntry(Resource{ID: uuid.NewString(), Name: "Browser", CreatedAt: time.Now().UTC(), State: "stopped", Control: "agent", Agent: worker})
		m.entries[e.resource.ID] = e
		m.byWorker[rowKey] = e
	}
	m.mu.Unlock()
	var rt Runtime
	err := m.withHost(ctx, func(h Host) error {
		var err error
		rt, err = h.Window(ctx, target)
		return err
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.deleted {
		e.mu.Unlock()
		_ = rt.Close()
		return errors.New("Browser not found")
	}
	if e.runtime != nil {
		_ = e.runtime.Close()
	} else {
		m.windows.Add(1)
	}
	e.runtime = rt
	oldKey := e.key
	e.key = key
	e.resource.Generation = uuid.NewString()
	e.resource.Target = target
	e.resource.State = "running"
	e.touches, e.handedBack = nil, nil
	e.epoch++
	e.mu.Unlock()
	m.mu.Lock()
	if oldKey != "" && oldKey != key && m.byKey[oldKey] == e {
		delete(m.byKey, oldKey)
	}
	m.byKey[key] = e
	m.mu.Unlock()
	return nil
}

// Viewed follows the page a window shows, as the extension reports it.
func (m *Manager) Viewed(key, target string) {
	m.mu.Lock()
	e := m.byKey[key]
	m.mu.Unlock()
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.runtime != nil {
		e.runtime.SetTarget(target)
	}
	e.resource.Target = target
	e.mu.Unlock()
}

// Closed takes a window off the Screen when the extension reports it closed.
func (m *Manager) Closed(key string) {
	m.mu.Lock()
	e := m.byKey[key]
	m.mu.Unlock()
	if e != nil {
		m.forget(e, false)
	}
}

// Forget closes a Worker's window and takes it off the Screen. The browser
// stops with its last window; the profile and its sign-ins stay.
func (m *Manager) Forget(worker string) {
	m.mu.Lock()
	e := m.byWorker[worker]
	m.mu.Unlock()
	if e != nil {
		m.forget(e, true)
	}
}

func (m *Manager) forget(e *entry, closeWindow bool) {
	m.mu.Lock()
	for w, x := range m.byWorker {
		if x == e {
			delete(m.byWorker, w)
		}
	}
	for k, x := range m.byKey {
		if x == e {
			delete(m.byKey, k)
		}
	}
	delete(m.entries, e.resource.ID)
	closer := m.closeWindow
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e.transition.Lock()
	defer e.transition.Unlock()
	_ = acquire(ctx, e)
	e.mu.Lock()
	if e.deleted {
		e.mu.Unlock()
		m.gateRelease(e)
		return
	}
	e.deleted = true
	e.owner = Owner{}
	if e.expiry != nil {
		e.expiry.Stop()
	}
	key := e.key
	m.dropRuntime(e)
	e.mu.Unlock()
	m.gateRelease(e)
	if closeWindow && closer != nil && key != "" {
		closer(key)
	}
	go m.stopIdleHost()
}

func (m *Manager) gateRelease(e *entry) {
	select {
	case e.gate <- struct{}{}:
	default:
	}
}

// SetWorkerLive tells the manager how to ask whether a Worker still exists;
// the windows of those that do not are closed.
func (m *Manager) SetWorkerLive(f func(worker string) bool) {
	m.mu.Lock()
	m.workerLive = f
	m.mu.Unlock()
}

// sweep closes the windows of Workers that have ended.
func (m *Manager) sweep(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}
		m.mu.Lock()
		live := m.workerLive
		workers := make(map[string]*entry, len(m.byWorker))
		for w, e := range m.byWorker {
			workers[w] = e
		}
		m.mu.Unlock()
		var gone []string
		for w, e := range workers {
			if strings.HasPrefix(w, "window:") {
				continue // no Worker to ask about
			}
			e.mu.Lock()
			person := e.owner.Kind == "human"
			e.mu.Unlock()
			if live != nil && !person && !live(w) {
				gone = append(gone, w)
			}
		}
		for _, w := range gone {
			m.Forget(w)
		}
	}
}

// dropRuntime closes the entry's window. Caller holds e.mu.
func (m *Manager) dropRuntime(e *entry) {
	if e.runtime == nil {
		return
	}
	_ = e.runtime.Close()
	e.runtime = nil
	m.windows.Add(-1)
}

// withHost runs f on the computer's browser, starting it first when it is not
// running. One call at a time: a second waits for the first to finish.
func (m *Manager) withHost(ctx context.Context, f func(Host) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.hostGate:
	}
	defer func() { m.hostGate <- struct{}{} }()
	if m.closed.Load() {
		return ErrUnavailable
	}
	if m.host != nil && !m.host.Alive() {
		_ = m.host.Close()
		m.host = nil
	}
	if m.host == nil {
		started := time.Now()
		h, err := m.backend.Start(ctx, filepath.Join(m.root, agentProfile))
		if err != nil {
			log.Printf("browser: starting the agent browser failed after %s: %v", time.Since(started).Round(time.Millisecond), err)
			return err
		}
		log.Printf("browser: agent browser started in %s", time.Since(started).Round(time.Millisecond))
		m.host = h
	}
	return f(m.host)
}

// stopIdleHost stops the browser once no agent has a window on it.
func (m *Manager) stopIdleHost() {
	<-m.hostGate
	defer func() { m.hostGate <- struct{}{} }()
	if m.host == nil || m.windows.Load() > 0 || time.Since(time.Unix(0, m.ensuredAt.Load())) < hostGrace {
		return
	}
	_ = m.host.Close()
	m.host = nil
	log.Printf("browser: agent browser stopped, no agent has a window")
}

func (m *Manager) notRunning(e *entry) error {
	if e.runtime != nil && e.runtime.Alive() {
		return nil
	}
	return fmt.Errorf("%w: it is not running", ErrUnavailable)
}

// control gives the person the window. New tool calls of the agent are refused
// from now; the ones running are waited for, up to drainWait.
func (m *Manager) control(ctx context.Context, e *entry, o Owner) (Response, error) {
	e.mu.Lock()
	if e.transferring {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if e.owner.ID != "" && e.owner != o {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if err := m.notRunning(e); err != nil {
		e.mu.Unlock()
		return Response{}, err
	}
	e.transferring = true
	e.mu.Unlock()
	if err := m.drainAgent(ctx, e); err != nil {
		e.mu.Lock()
		e.transferring = false
		e.mu.Unlock()
		return Response{}, err
	}
	if err := acquire(ctx, e); err != nil {
		e.mu.Lock()
		e.transferring = false
		e.mu.Unlock()
		return Response{}, err
	}
	defer unlock(e)
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() { e.transferring = false }()
	if err := m.notRunning(e); err != nil {
		return Response{}, err
	}
	if err := e.runtime.Release(ctx); err != nil {
		log.Printf("browser: lifting held input of %s failed: %v", e.resource.Agent, err)
	}
	e.epoch++
	e.owner = o
	e.humanFrom = time.Now()
	e.expires = time.Now().Add(leaseLifetime)
	m.armExpiryLocked(e)
	lease := Lease{Epoch: e.epoch, Generation: e.resource.Generation, Target: e.runtime.Target(), Kind: o.Kind, ExpiresAt: e.expires}
	r := snapshot(e)
	r.Control = o.Kind
	return Response{Lease: &lease, Resource: &r}, nil
}

// drainAgent waits for the agent's running tool calls to end, up to drainWait.
func (m *Manager) drainAgent(ctx context.Context, e *entry) error {
	deadline := time.Now().Add(drainWait)
	for {
		e.mu.Lock()
		busy := e.agentBusy
		e.mu.Unlock()
		if busy == 0 || time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// command is the person's input under their lease.
func (m *Manager) command(ctx context.Context, e *entry, o Owner, l Lease, c Command) (Response, error) {
	if err := acquire(ctx, e); err != nil {
		return Response{}, err
	}
	defer unlock(e)
	e.mu.Lock()
	if e.deleted || e.owner != o || l.Epoch != e.epoch || l.Generation != e.resource.Generation || time.Now().After(e.expires) || e.transferring || e.runtime == nil {
		e.mu.Unlock()
		return Response{}, ErrLease
	}
	e.expires = time.Now().Add(leaseLifetime)
	m.armExpiryLocked(e)
	if c.Kind == "heartbeat" {
		e.mu.Unlock()
		return Response{}, nil
	}
	rt := e.runtime
	e.mu.Unlock()
	actionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var result json.RawMessage
	switch c.Kind {
	case "input":
		if c.Input == nil {
			return Response{}, errors.New("Input is required")
		}
		if err := rt.Input(actionCtx, *c.Input); err != nil {
			return Response{}, err
		}
	case "navigate", "tabs", "select_tab", "new_tab":
		var err error
		if result, err = rt.Command(actionCtx, c); err != nil {
			return Response{}, err
		}
	default:
		return Response{}, errors.New("Unsupported browser command")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	l.Target = rt.Target()
	l.ExpiresAt = e.expires
	r := snapshot(e)
	return Response{Resource: &r, Lease: &l, Result: result}, nil
}
func (m *Manager) releaseMatching(ctx context.Context, e *entry, match func(Owner) bool) error {
	e.mu.Lock()
	if !match(e.owner) {
		e.mu.Unlock()
		return nil
	}
	e.transferring = true
	e.mu.Unlock()
	if err := acquire(ctx, e); err != nil {
		e.mu.Lock()
		e.transferring = false
		e.mu.Unlock()
		return err
	}
	defer unlock(e)
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() { e.transferring = false }()
	if match(e.owner) {
		if e.owner.Kind == "human" {
			// Handing back is the person saying carry on to a stopped agent,
			// and the agent hears what happened while it was away.
			e.halted = nil
			e.handedBack = &span{from: e.humanFrom, to: time.Now()}
		}
		e.owner = Owner{}
		e.epoch++
		if e.runtime != nil {
			if err := e.runtime.Release(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) ReleaseOwner(o Owner) { m.ReleaseWhere(func(x Owner) bool { return x == o }) }
func (m *Manager) ReleaseWhere(match func(Owner) bool) {
	for _, e := range m.allEntries() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		e.transition.Lock()
		_ = m.releaseMatching(ctx, e, match)
		e.transition.Unlock()
		cancel()
	}
}
func (m *Manager) OpenStream(ctx context.Context, id string) (Stream, error) {
	e, err := m.get(id)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime == nil || !e.runtime.Alive() {
		return nil, ErrUnavailable
	}
	if e.viewer {
		return nil, errors.New("This browser already has an open viewer")
	}
	stream, err := e.runtime.Stream(ctx)
	if err != nil {
		return nil, err
	}
	e.viewer = true
	return &ownedStream{Stream: stream, release: func() { e.mu.Lock(); e.viewer = false; e.mu.Unlock() }}, nil
}

type ownedStream struct {
	Stream
	once    sync.Once
	release func()
}

func (s *ownedStream) Close() error { err := s.Stream.Close(); s.once.Do(s.release); return err }

// armExpiryLocked aims the entry's timer at its lease's end, replacing any
// earlier aim. Every renewal calls it with e.mu held; nothing sweeps.
func (m *Manager) armExpiryLocked(e *entry) {
	wait := time.Until(e.expires)
	if e.expiry == nil {
		e.expiry = time.AfterFunc(wait, func() { m.expireLease(e) })
		return
	}
	e.expiry.Reset(wait)
}

// expireLease releases the entry's lease if it has run out. A renewal that
// landed after the timer was aimed leaves the lease alive and the timer
// aimed again, so a stale firing is harmless.
func (m *Manager) expireLease(e *entry) {
	select {
	case <-m.done:
		return
	default:
	}
	e.transition.Lock()
	defer e.transition.Unlock()
	e.mu.Lock()
	owner, expires, deleted := e.owner, e.expires, e.deleted
	if owner.ID != "" && !deleted && time.Now().Before(expires) {
		m.armExpiryLocked(e)
	}
	e.mu.Unlock()
	if owner.ID == "" || deleted || time.Now().Before(expires) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = m.releaseMatching(ctx, e, func(x Owner) bool { return x == owner && time.Now().After(e.expires) })
}
func (m *Manager) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(m.done)
	for _, e := range m.allEntries() {
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		e.transition.Lock()
		_ = acquire(ctx, e)
		e.mu.Lock()
		e.deleted = true
		e.owner = Owner{}
		if e.expiry != nil {
			e.expiry.Stop()
		}
		m.dropRuntime(e)
		e.mu.Unlock()
		m.gateRelease(e)
		e.transition.Unlock()
		c()
	}
	// With the windows gone, the browser itself.
	<-m.hostGate
	var err error
	if m.host != nil {
		err = m.host.Close()
		m.host = nil
	}
	m.hostGate <- struct{}{}
	return err
}
