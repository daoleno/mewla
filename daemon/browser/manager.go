package browser

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Screens is the agent screen the browser runs on: a display for Chrome, and
// a monitor on it for each agent whose window is open.
type Screens interface {
	// DisplayEnv starts the agent screen if it is not up and returns its X
	// display number and authority file, for the programs that run on it.
	DisplayEnv(ctx context.Context) (number int, xauthority string, err error)
	// Monitor is the Worker's monitor, given to it if it has none.
	Monitor(ctx context.Context, worker string) (image.Rectangle, error)
	// Windows says how many browser windows the Worker has on its monitor.
	Windows(worker string, n int)
}

var validID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

// agentProfile is the one profile agents browse with, under the browsers directory.
const agentProfile = "agent"

// Manager keeps the computer's one agent browser: starts it when an agent
// asks for it, tells the screen which Workers have windows in it, and stops
// it when none has.
type Manager struct {
	mu          sync.Mutex
	root        string
	backend     Backend
	screens     Screens
	keys        map[string]string // the extension's id for a window -> the Worker it is for
	closeWindow func(key string)
	switchHook  func()
	closed      atomic.Bool
	done        chan struct{}

	virtual    atomic.Bool // Settings.VirtualScreen
	settingsMu sync.Mutex  // serializes writes of settings.json

	// The computer's one browser. Starting and stopping it go through
	// hostGate one at a time, so an agent that asks while another's start is
	// under way waits for it instead of failing.
	hostGate   chan struct{}
	host       Host
	ensuredAt  atomic.Int64 // unix nanoseconds of the last EnsureHost
	workerLive func(worker string) bool
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

func New(root string, backend Backend, screens Screens) (*Manager, error) {
	if err := privateDir(root); err != nil {
		return nil, err
	}
	if err := adoptOldProfile(root); err != nil {
		return nil, err
	}
	m := &Manager{root: root, backend: backend, screens: screens, keys: map[string]string{}, done: make(chan struct{}), hostGate: make(chan struct{}, 1)}
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
		var r struct {
			CreatedAt time.Time `json:"created_at"`
		}
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

// Handle answers the app's settings requests.
func (m *Manager) Handle(ctx context.Context, q Request) (Response, error) {
	switch q.Action {
	case "settings":
		if q.Enabled != nil {
			if err := m.setVirtualScreen(*q.Enabled); err != nil {
				return Response{}, err
			}
		}
		on := m.VirtualScreen()
		c := m.backend.Capability(ctx)
		return Response{VirtualScreen: &on, Capability: &c}, nil
	}
	return Response{}, errors.New("Unknown browser operation")
}

// Capability is whether this computer can run the agent browser, and why not.
func (m *Manager) Capability(ctx context.Context) Capability { return m.backend.Capability(ctx) }

// sweepEvery is how often the windows of ended Workers are looked for; a variable so tests can shorten it.
var sweepEvery = 10 * time.Second

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

// EnsureHost starts the agent browser (the agent screen, Chrome and the
// extension) when it is not running, and waits for it. Agents that ask while
// it is starting wait for the same start.
func (m *Manager) EnsureHost(ctx context.Context) error {
	m.ensuredAt.Store(time.Now().UnixNano())
	err := m.withHost(ctx)
	if err == nil {
		time.AfterFunc(hostGrace+time.Second, func() { m.stopIdleHost() })
	}
	return err
}

// Monitor is where the Worker's window goes on the agent screen.
func (m *Manager) Monitor(ctx context.Context, worker string) (image.Rectangle, error) {
	if m.screens == nil {
		return image.Rectangle{}, ErrUnavailable
	}
	return m.screens.Monitor(ctx, workerName(worker, ""))
}

// workerName is the name a window's monitor goes by: its Worker's, or for a
// window with no Worker its own.
func workerName(worker, key string) string {
	if worker != "" {
		return worker
	}
	return "window:" + key
}

// Opened registers the window the extension opened for a Worker, so the
// Screen shows it on the Worker's monitor. key is the extension's id for the
// window. A Worker has one window; one it opens again replaces the old one.
func (m *Manager) Opened(worker, key string) error {
	if m.closed.Load() {
		return ErrUnavailable
	}
	name := workerName(worker, key)
	m.mu.Lock()
	for k, w := range m.keys {
		if w == name && k != key {
			delete(m.keys, k)
		}
	}
	m.keys[key] = name
	n := m.countLocked(name)
	m.mu.Unlock()
	if m.screens != nil {
		m.screens.Windows(name, n)
	}
	return nil
}

func (m *Manager) countLocked(worker string) int {
	n := 0
	for _, w := range m.keys {
		if w == worker {
			n++
		}
	}
	return n
}

// Closed takes a window off the Screen when the extension reports it closed.
func (m *Manager) Closed(key string) {
	m.mu.Lock()
	name, ok := m.keys[key]
	delete(m.keys, key)
	n := m.countLocked(name)
	m.mu.Unlock()
	if ok && m.screens != nil {
		m.screens.Windows(name, n)
	}
	go m.stopIdleHost()
}

// Forget closes a Worker's windows. The browser stops with its last window;
// the profile and its sign-ins stay.
func (m *Manager) Forget(worker string) {
	m.mu.Lock()
	var keys []string
	for k, w := range m.keys {
		if w == worker {
			keys = append(keys, k)
			delete(m.keys, k)
		}
	}
	closer := m.closeWindow
	m.mu.Unlock()
	if closer != nil {
		for _, k := range keys {
			closer(k)
		}
	}
	if m.screens != nil {
		m.screens.Windows(worker, 0)
	}
	go m.stopIdleHost()
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
		workers := map[string]bool{}
		for _, w := range m.keys {
			workers[w] = true
		}
		m.mu.Unlock()
		for w := range workers {
			if strings.HasPrefix(w, "window:") {
				continue // no Worker to ask about
			}
			if live != nil && !live(w) {
				m.Forget(w)
			}
		}
	}
}

// withHost makes sure the computer's browser is running, starting it if not.
// One call at a time: a second waits for the first to finish.
func (m *Manager) withHost(ctx context.Context) error {
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
		m.mu.Lock() // its windows went with it
		gone := map[string]bool{}
		for k, w := range m.keys {
			gone[w] = true
			delete(m.keys, k)
		}
		m.mu.Unlock()
		for w := range gone {
			if m.screens != nil {
				m.screens.Windows(w, 0)
			}
		}
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
	return nil
}

// stopIdleHost stops the browser once no agent has a window on it.
func (m *Manager) stopIdleHost() {
	<-m.hostGate
	defer func() { m.hostGate <- struct{}{} }()
	m.mu.Lock()
	windows := len(m.keys)
	m.mu.Unlock()
	if m.host == nil || windows > 0 || time.Since(time.Unix(0, m.ensuredAt.Load())) < hostGrace {
		return
	}
	_ = m.host.Close()
	m.host = nil
	log.Printf("browser: agent browser stopped, no agent has a window")
}

// Close ends the browser.
func (m *Manager) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(m.done)
	<-m.hostGate
	defer func() { m.hostGate <- struct{}{} }()
	if m.host != nil {
		_ = m.host.Close()
		m.host = nil
	}
	return nil
}
