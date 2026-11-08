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
	owner        Owner
	epoch        uint64
	expires      time.Time
	expiry       *time.Timer // fires when expires passes; see armExpiryLocked
	transferring bool
	uncertain    bool
	viewer       bool

	// What the person did in a browser an agent holds, told to the agent with
	// its next result; see touch.go.
	touches    []touch
	agentBusy  bool            // an agent's action is running
	halted     map[string]bool // Workers the person stopped
	humanFrom  time.Time       // when the person took the lease
	handedBack *span           // the person's last turn, not yet told to the agent
	lastAgent  string
}

// Lock order: m.mu guards only the two maps and workerLive, and is never held
// while an entry's lock is taken, nor taken while one is held. Everything an
// entry's code needs from the manager (the switch, whether it is closed) is
// atomic for that reason; a deadlock here once froze every agent at once.
type Manager struct {
	mu       sync.Mutex
	root     string
	backend  Backend
	entries  map[string]*entry // agents' windows, by window id
	byWorker map[string]*entry // the same, by the Worker whose window it is
	closed   atomic.Bool
	done     chan struct{}

	virtual    atomic.Bool // Settings.VirtualScreen
	settingsMu sync.Mutex  // serializes writes of settings.json

	// The computer's one browser. Starting and stopping it, and opening a
	// window on it, go through hostGate one at a time, so an agent that asks
	// while another's start is under way waits for it instead of failing.
	hostGate   chan struct{}
	host       Host
	windows    atomic.Int32
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
	m := &Manager{root: root, backend: backend, entries: map[string]*entry{}, byWorker: map[string]*entry{}, done: make(chan struct{}), hostGate: make(chan struct{}, 1)}
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
	if e.runtime != nil && !e.runtime.Alive() {
		r.State = "stopped"
	}
	r.Control = "idle"
	if e.transferring {
		r.Control = "quiescing"
	} else if e.owner.ID != "" {
		r.Control = e.owner.Kind
	}
	// The agent on the row: its Worker while it holds the browser, and still
	// that Worker while the person does, so the list keeps the row.
	switch {
	case e.owner.Kind == "agent":
		r.Agent = workerOf(e.owner)
	case e.owner.Kind == "human":
		r.Agent = e.lastAgent
	}
	if e.uncertain {
		r.State = "needs_restart"
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
func (m *Manager) Handle(ctx context.Context, o Owner, q Request) (Response, error) {
	if (o.Kind != "human" && o.Kind != "agent") || o.ID == "" {
		return Response{}, errors.New("Browser owner is required")
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
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		if q.Enabled != nil {
			if err := m.setVirtualScreen(*q.Enabled); err != nil {
				return Response{}, err
			}
		}
		on := m.VirtualScreen()
		return Response{VirtualScreen: &on}, nil
	}
	var e *entry
	var err error
	if o.Kind == "agent" {
		// An agent's window is found by its Worker, not by an id it was given:
		// the id would be lost with the first daemon restart.
		e, err = m.agentWindow(o, q.Action == "control")
		if e == nil && err == nil {
			if q.Action == "release" {
				return Response{}, nil
			}
			return Response{}, ErrLease
		}
	} else {
		e, err = m.get(q.ID)
	}
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
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "control":
		r, err := m.control(ctx, e, o)
		if err == nil && o.Kind == "agent" {
			r.Notes = m.notesFor(ctx, e)
		}
		return r, err
	case "release":
		err = m.releaseMatching(ctx, e, func(x Owner) bool { return x == o })
		return Response{}, err
	case "touch":
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		return m.touch(ctx, e, o, q)
	case "halt":
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
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
		r, err := m.command(ctx, e, o, *q.Lease, *q.Command)
		if err == nil && o.Kind == "agent" && q.Command.Kind != "heartbeat" {
			r.Notes = m.notesFor(ctx, e)
		}
		return r, err
	default:
		return Response{}, errors.New("Unknown browser operation")
	}
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

// agentWindow is the window of the Worker an agent owner belongs to; with
// create, it makes the entry when the Worker has none (nothing is opened yet).
func (m *Manager) agentWindow(o Owner, create bool) (*entry, error) {
	worker := workerOf(o)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return nil, ErrUnavailable
	}
	if e := m.byWorker[worker]; e != nil {
		return e, nil
	}
	if !create {
		return nil, nil
	}
	if len(m.entries) >= maxWindows {
		return nil, fmt.Errorf("%w: %d agents already have windows; wait for one to finish", ErrBusy, maxWindows)
	}
	e := newEntry(Resource{ID: uuid.NewString(), Name: "Browser", CreatedAt: time.Now().UTC(), State: "stopped", Control: "idle", Agent: worker})
	m.entries[e.resource.ID] = e
	m.byWorker[worker] = e
	return e, nil
}

// sweepEvery is how often the windows of ended Workers are looked for; a variable so tests can shorten it.
var sweepEvery = 10 * time.Second

// maxWindows is the most agent windows one computer keeps.
const maxWindows = 8

// Forget closes a Worker's window and takes it off the Screen. The browser
// stops with its last window; the profile and its sign-ins stay.
func (m *Manager) Forget(worker string) {
	m.mu.Lock()
	e := m.byWorker[worker]
	if e == nil {
		m.mu.Unlock()
		return
	}
	delete(m.byWorker, worker)
	delete(m.entries, e.resource.ID)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e.transition.Lock()
	defer e.transition.Unlock()
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
	if m.host == nil || m.windows.Load() > 0 {
		return
	}
	_ = m.host.Close()
	m.host = nil
	log.Printf("browser: agent browser stopped, no agent has a window")
}

// start opens the entry's window if it has none that works; a working one is
// left as it is. Only this window is opened again, never the browser under
// other agents' windows, unless the browser itself is gone.
func (m *Manager) start(ctx context.Context, e *entry) error {
	if err := acquire(ctx, e); err != nil {
		return err
	}
	defer unlock(e)
	e.mu.Lock()
	defer e.mu.Unlock()
	// A window whose last action could not be drained is open but cannot be
	// trusted: opening it again is the way out, and the only one.
	if e.runtime != nil && e.runtime.Alive() && !e.uncertain {
		return nil
	}
	m.dropRuntime(e)
	err := m.withHost(ctx, func(h Host) error {
		runtime, err := h.Window(ctx)
		if err != nil {
			return err
		}
		m.windows.Add(1)
		e.runtime = runtime
		return nil
	})
	if err != nil {
		log.Printf("browser: opening a window for %s failed: %v", e.resource.Agent, err)
		return err
	}
	e.resource.Generation = uuid.NewString()
	e.resource.Target = e.runtime.Target()
	e.resource.State = "running"
	e.uncertain = false
	e.owner = Owner{}
	e.halted = nil
	e.touches, e.handedBack = nil, nil
	e.epoch++
	return nil
}

// notRunning says why control cannot go ahead on this browser, in words the
// agent can act on, or nil when it is running and trusted. Caller holds e.mu.
func (m *Manager) notRunning(e *entry, o Owner) error {
	switch {
	case e.uncertain:
		return ErrUncertain
	case e.runtime != nil && e.runtime.Alive():
		return nil
	case o.Kind == "agent" && !m.VirtualScreen():
		return fmt.Errorf("%w: it is not running and the virtual screen is off, so no browser starts for agents; the person turns it on in Settings, Labs, Virtual screen", ErrUnavailable)
	}
	return fmt.Errorf("%w: it is not running", ErrUnavailable)
}

func (m *Manager) control(ctx context.Context, e *entry, o Owner) (Response, error) {
	if o.Kind == "agent" && m.VirtualScreen() {
		// With the virtual screen on, an agent's window opens when it first asks
		// for control, not when the Worker launches, and the browser under it
		// starts then if nothing else has started it.
		e.mu.Lock()
		halted := e.halted[workerOf(o)]
		// Not running, or running but stuck "uncertain" after an action that
		// could not be drained (nobody can clear that from the phone but a
		// restart). The person's own session and another agent's live lease
		// are left alone.
		down := e.runtime == nil || !e.runtime.Alive()
		stuck := e.uncertain && !e.transferring && e.owner.Kind != "human" &&
			(e.owner.ID == "" || e.owner == o || time.Now().After(e.expires))
		e.mu.Unlock()
		if (down || stuck) && !halted {
			if err := m.start(ctx, e); err != nil {
				return Response{}, err
			}
		}
	}
	e.mu.Lock()
	if e.transferring {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if o.Kind == "agent" && e.halted[workerOf(o)] {
		e.mu.Unlock()
		return Response{}, errHalted
	}
	if o.Kind == "agent" && e.owner.Kind == "human" {
		e.mu.Unlock()
		return Response{}, errPersonInControl
	}
	if o.Kind == "agent" && e.owner.ID != "" && e.owner != o {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if err := m.notRunning(e, o); err != nil {
		e.mu.Unlock()
		return Response{}, err
	}
	e.transferring = true
	e.mu.Unlock()
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
	if e.uncertain {
		return Response{}, ErrUncertain
	}
	if err := m.notRunning(e, o); err != nil {
		return Response{}, err
	}
	if err := e.runtime.Release(ctx); err != nil {
		e.uncertain = true
		return Response{}, ErrUncertain
	}
	e.epoch++
	e.owner = o
	if o.Kind == "human" {
		e.humanFrom = time.Now()
	} else {
		if e.lastAgent != workerOf(o) {
			e.touches, e.handedBack = nil, nil
		}
		e.lastAgent = workerOf(o)
	}
	e.expires = time.Now().Add(leaseLifetime)
	m.armExpiryLocked(e)
	lease := Lease{Epoch: e.epoch, Generation: e.resource.Generation, Target: e.resource.Target, Kind: o.Kind, ExpiresAt: e.expires}
	r := snapshot(e)
	r.Control = o.Kind
	return Response{Lease: &lease, Resource: &r}, nil
}
func (m *Manager) command(ctx context.Context, e *entry, o Owner, l Lease, c Command) (Response, error) {
	if err := acquire(ctx, e); err != nil {
		return Response{}, err
	}
	defer unlock(e)
	e.mu.Lock()
	if o.Kind == "agent" && !e.deleted {
		if e.halted[workerOf(o)] {
			e.mu.Unlock()
			return Response{}, errHalted
		}
		if e.owner.Kind == "human" {
			e.mu.Unlock()
			return Response{}, errPersonInControl
		}
	}
	if e.deleted || e.owner != o || l.Epoch != e.epoch || l.Generation != e.resource.Generation || l.Target != e.resource.Target || time.Now().After(e.expires) || e.transferring || e.uncertain || e.runtime == nil {
		e.mu.Unlock()
		return Response{}, ErrLease
	}
	if c.Kind == "heartbeat" {
		e.expires = time.Now().Add(leaseLifetime)
		m.armExpiryLocked(e)
		e.mu.Unlock()
		return Response{}, nil
	}
	rt := e.runtime
	e.expires = time.Now().Add(leaseLifetime)
	m.armExpiryLocked(e)
	e.agentBusy = true
	e.mu.Unlock()
	actionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := rt.Command(actionCtx, c)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentBusy = false
	if errors.Is(err, ErrUncertain) || actionCtx.Err() != nil {
		e.uncertain = true
		return Response{}, ErrUncertain
	}
	if err != nil {
		return Response{}, err
	}
	e.resource.Target = rt.Target()
	if e.transferring || e.owner != o {
		return Response{}, ErrLease
	} // Do not deliver Agent reads after takeover request.
	l.Target = e.resource.Target
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
		e.uncertain = true
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
				e.uncertain = true
				return ErrUncertain
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
