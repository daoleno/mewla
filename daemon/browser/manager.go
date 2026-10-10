package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	transferring bool
	uncertain    bool
	viewer       bool
}
type Manager struct {
	mu      sync.Mutex
	root    string
	backend Backend
	entries map[string]*entry
	closed  bool
	done    chan struct{}
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
	go m.expire()
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
		return Response{Resources: resources, Capability: &c}, nil
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
		if len(m.entries) >= 8 {
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
	if q.Action != "command" {
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
		if err = acquire(ctx, e); err != nil {
			return Response{}, err
		}
		defer unlock(e)
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.runtime != nil && e.runtime.Alive() {
			r := snapshot(e)
			return Response{Resource: &r}, nil
		}
		if e.runtime != nil {
			_ = e.runtime.Close()
			e.runtime = nil
		}
		runtime, err := m.backend.Start(ctx, filepath.Join(m.root, q.ID))
		if err != nil {
			return Response{}, err
		}
		e.runtime = runtime
		e.resource.Generation = uuid.NewString()
		e.resource.Target = runtime.Target()
		e.resource.State = "running"
		e.uncertain = false
		e.owner = Owner{}
		e.epoch++
		r := snapshot(e)
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
		return m.control(ctx, e, o)
	case "release":
		err = m.releaseMatching(ctx, e, func(x Owner) bool { return x == o })
		return Response{}, err
	case "command":
		if q.Command == nil || q.Lease == nil {
			return Response{}, ErrLease
		}
		return m.command(ctx, e, o, *q.Lease, *q.Command)
	default:
		return Response{}, errors.New("Unknown browser operation")
	}
}
func (m *Manager) control(ctx context.Context, e *entry, o Owner) (Response, error) {
	e.mu.Lock()
	if e.transferring {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if o.Kind == "agent" && (!e.resource.AllowAgents || (e.owner.ID != "" && e.owner != o)) {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	if e.uncertain || e.runtime == nil || !e.runtime.Alive() {
		e.mu.Unlock()
		return Response{}, ErrUnavailable
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
	if e.runtime == nil || !e.runtime.Alive() {
		return Response{}, ErrUnavailable
	}
	if err := e.runtime.Release(ctx); err != nil {
		e.uncertain = true
		return Response{}, ErrUncertain
	}
	e.epoch++
	e.owner = o
	e.expires = time.Now().Add(LeaseLifetime)
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
	if e.deleted || e.owner != o || l.Epoch != e.epoch || l.Generation != e.resource.Generation || l.Target != e.resource.Target || time.Now().After(e.expires) || e.transferring || e.uncertain || e.runtime == nil {
		e.mu.Unlock()
		return Response{}, ErrLease
	}
	if c.Kind == "heartbeat" {
		e.expires = time.Now().Add(LeaseLifetime)
		e.mu.Unlock()
		return Response{}, nil
	}
	if o.Kind == "agent" && !e.resource.AllowAgents {
		e.mu.Unlock()
		return Response{}, ErrLease
	}
	rt := e.runtime
	e.expires = time.Now().Add(LeaseLifetime)
	e.mu.Unlock()
	actionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := rt.Command(actionCtx, c)
	e.mu.Lock()
	defer e.mu.Unlock()
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
func (m *Manager) expire() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			m.mu.Lock()
			list := []*entry{}
			for _, e := range m.entries {
				list = append(list, e)
			}
			m.mu.Unlock()
			for _, e := range list {
				e.transition.Lock()
				e.mu.Lock()
				expired := e.owner.ID != "" && time.Now().After(e.expires)
				o := e.owner
				e.mu.Unlock()
				if expired {
					ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
					_ = m.releaseMatching(ctx, e, func(x Owner) bool { return x == o && time.Now().After(e.expires) })
					c()
				}
				e.transition.Unlock()
			}
		}
	}
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
		e.mu.Unlock()
		e.transition.Unlock()
		c()
	}
	return errors.Join(errs...)
}
