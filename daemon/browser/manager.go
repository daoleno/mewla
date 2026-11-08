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
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
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
	reservedAt time.Time // handed to a launching Worker; see AutoAttachment
}
type Manager struct {
	mu      sync.Mutex
	root    string
	backend Backend
	entries map[string]*entry
	closed  bool
	done    chan struct{}

	settings Settings
}

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
	m := &Manager{root: root, backend: backend, entries: map[string]*entry{}, done: make(chan struct{})}
	files, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !validID.MatchString(f.Name()) {
			continue
		}
		path := filepath.Join(root, f.Name())
		if f.Type()&os.ModeSymlink != 0 || !f.IsDir() {
			return nil, errors.New("Invalid browser resource directory")
		}
		meta := filepath.Join(path, "resource.json")
		info, err := os.Lstat(meta)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("Invalid browser metadata file")
		}
		data, err := os.ReadFile(meta)
		if err != nil {
			return nil, err
		}
		var r Resource
		if json.Unmarshal(data, &r) != nil || r.ID != f.Name() {
			return nil, errors.New("Invalid browser resource metadata")
		}
		r.State = "stopped"
		r.Generation = ""
		r.Control = "idle"
		r.Target = ""
		m.entries[r.ID] = newEntry(r)
	}
	m.settings = loadSettings(root)
	return m, nil
}
func newEntry(r Resource) *entry {
	e := &entry{resource: r, gate: make(chan struct{}, 1)}
	e.gate <- struct{}{}
	return e
}
func (m *Manager) save(e *entry) error {
	// Caller holds entry.mu. Atomic metadata replacement never touches profile data.
	path := filepath.Join(m.root, e.resource.ID)
	if err := privateDir(path); err != nil {
		return err
	}
	b, err := json.Marshal(e.resource)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(path, "resource.json"), b, 0o600)
}
func (m *Manager) get(id string) (*entry, error) {
	if !validID.MatchString(id) {
		return nil, errors.New("Invalid browser resource")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrUnavailable
	}
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
		m.mu.Lock()
		defer m.mu.Unlock()
		resources := []Resource{}
		for _, e := range m.entries {
			e.mu.Lock()
			if o.Kind == "human" || e.resource.AllowAgents {
				resources = append(resources, snapshot(e))
			}
			e.mu.Unlock()
		}
		sort.Slice(resources, func(i, j int) bool { return resources[i].CreatedAt.Before(resources[j].CreatedAt) })
		c := m.backend.Capability(ctx)
		on := m.settings.VirtualScreen
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
	if q.Action == "create" {
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		name := strings.TrimSpace(q.Name)
		if name == "" || len(name) > 80 {
			return Response{}, errors.New("Choose a browser name of 1–80 bytes")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Response{}, ErrUnavailable
		}
		if len(m.entries) >= maxBrowsers {
			return Response{}, errors.New("This server supports up to eight browser profiles")
		}
		// Agents of the same server owner may use a new browser by default; the
		// lease still decides who types, and grant(false) remains a revocation.
		e := newEntry(Resource{ID: uuid.NewString(), Name: name, CreatedAt: time.Now().UTC(), State: "stopped", Control: "idle", AllowAgents: true})
		if err := m.save(e); err != nil {
			return Response{}, err
		}
		m.entries[e.resource.ID] = e
		r := snapshot(e)
		return Response{Resource: &r}, nil
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
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "start":
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		if err = m.start(ctx, e); err != nil {
			return Response{}, err
		}
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "stop", "delete":
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		if err = m.stop(ctx, e); err != nil {
			return Response{}, err
		}
		if q.Action == "delete" {
			m.mu.Lock()
			defer m.mu.Unlock()
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.runtime != nil {
				return Response{}, ErrBusy
			}
			e.deleted = true
			if err := os.RemoveAll(filepath.Join(m.root, q.ID)); err != nil {
				e.deleted = false
				return Response{}, err
			}
			if e.expiry != nil {
				e.expiry.Stop()
			}
			delete(m.entries, q.ID)
			return Response{}, nil
		}
		e.mu.Lock()
		r := snapshot(e)
		e.mu.Unlock()
		return Response{Resource: &r}, nil
	case "grant":
		if o.Kind != "human" {
			return Response{}, ErrLease
		}
		if !q.AllowAgents {
			if err = m.releaseMatching(ctx, e, func(x Owner) bool { return x.Kind == "agent" }); err != nil {
				return Response{}, err
			}
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		before := e.resource.AllowAgents
		e.resource.AllowAgents = q.AllowAgents
		if err = m.save(e); err != nil {
			e.resource.AllowAgents = before
			return Response{}, err
		}
		r := snapshot(e)
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

// start brings the browser up if it is not running; a running one is left as it is.
func (m *Manager) start(ctx context.Context, e *entry) error {
	if err := acquire(ctx, e); err != nil {
		return err
	}
	defer unlock(e)
	e.mu.Lock()
	defer e.mu.Unlock()
	// A runtime whose last action could not be drained is running but cannot
	// be trusted: starting it again is the way out, and the only one.
	if e.runtime != nil && e.runtime.Alive() && !e.uncertain {
		return nil
	}
	if e.runtime != nil {
		_ = e.runtime.Close()
		e.runtime = nil
	}
	runtime, err := m.backend.Start(ctx, filepath.Join(m.root, e.resource.ID))
	if err != nil {
		log.Printf("browser: starting %q (%s) failed: %v", e.resource.Name, e.resource.ID, err)
		return err
	}
	e.runtime = runtime
	e.resource.Generation = uuid.NewString()
	e.resource.Target = runtime.Target()
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
	case o.Kind == "agent" && !e.resource.AllowAgents:
		return fmt.Errorf("%w: agent access is off for this browser", ErrUnavailable)
	case o.Kind == "agent" && !m.VirtualScreen():
		return fmt.Errorf("%w: it is not running and the virtual screen is off, so it does not start for agents; the person can open it in Mewla Browser", ErrUnavailable)
	}
	return fmt.Errorf("%w: it is not running", ErrUnavailable)
}

func (m *Manager) control(ctx context.Context, e *entry, o Owner) (Response, error) {
	if o.Kind == "agent" && m.VirtualScreen() {
		// With the virtual screen on, a browser an agent is attached to starts when
		// the agent first asks for it, not when the Worker launches.
		e.mu.Lock()
		allowed, halted := e.resource.AllowAgents, e.halted[workerOf(o)]
		// Not running, or running but stuck "uncertain" after an action that
		// could not be drained (nobody can clear that from the phone but a
		// restart). The person's own session and another agent's live lease
		// are left alone.
		down := e.runtime == nil || !e.runtime.Alive()
		stuck := e.uncertain && !e.transferring && e.owner.Kind != "human" &&
			(e.owner.ID == "" || e.owner == o || time.Now().After(e.expires))
		e.mu.Unlock()
		if (down || stuck) && allowed && !halted {
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
	if o.Kind == "agent" && (!e.resource.AllowAgents || (e.owner.ID != "" && e.owner != o)) {
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
	if o.Kind == "agent" && !e.resource.AllowAgents {
		e.mu.Unlock()
		return Response{}, ErrLease
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
	m.mu.Lock()
	entries := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.Unlock()
	for _, e := range entries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		e.transition.Lock()
		_ = m.releaseMatching(ctx, e, match)
		e.transition.Unlock()
		cancel()
	}
}
func (m *Manager) stop(ctx context.Context, e *entry) error {
	e.mu.Lock()
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
	if e.runtime != nil {
		if err := e.runtime.Close(); err != nil {
			return err
		}
		e.runtime = nil
	}
	e.owner = Owner{}
	e.epoch++
	e.resource.State = "stopped"
	e.resource.Target = ""
	e.resource.Generation = ""
	e.uncertain = false
	return nil
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
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.done)
	entries := []*entry{}
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.Unlock()
	var errs []error
	for _, e := range entries {
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		e.transition.Lock()
		if err := m.stop(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("close browser: %w", err))
		}
		e.mu.Lock()
		e.deleted = true
		if e.expiry != nil {
			e.expiry.Stop()
		}
		e.mu.Unlock()
		e.transition.Unlock()
		c()
	}
	return errors.Join(errs...)
}
