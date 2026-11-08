package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBackend stands in for the one browser: Start makes a host, Window opens
// a window on it.
type fakeBackend struct {
	starts    atomic.Int32
	startErr  error
	startGate chan struct{} // when set, Start waits on it
	mu        sync.Mutex
	host      *fakeHost
	windows   []*fakeRuntime
}

func (b *fakeBackend) Capability(context.Context) Capability { return Capability{Available: true} }
func (b *fakeBackend) Start(ctx context.Context, _ string) (Host, error) {
	b.starts.Add(1)
	if b.startGate != nil {
		select {
		case <-b.startGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if b.startErr != nil {
		return nil, b.startErr
	}
	h := &fakeHost{b: b}
	h.live.Store(true)
	b.mu.Lock()
	b.host = h
	b.mu.Unlock()
	return h, nil
}

// last is the window opened most recently.
func (b *fakeBackend) last() *fakeRuntime {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.windows[len(b.windows)-1]
}
func (b *fakeBackend) opened() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.windows)
}
func (b *fakeBackend) hostAlive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.host != nil && b.host.live.Load()
}

type fakeHost struct {
	b    *fakeBackend
	live atomic.Bool
}

func (h *fakeHost) Alive() bool  { return h.live.Load() }
func (h *fakeHost) Close() error { h.live.Store(false); return nil }
func (h *fakeHost) Window(context.Context) (Runtime, error) {
	if !h.live.Load() {
		return nil, ErrUnavailable
	}
	r := &fakeRuntime{host: h}
	r.open.Store(true)
	h.b.mu.Lock()
	r.target = fmt.Sprintf("tab-%d", len(h.b.windows)+1)
	h.b.windows = append(h.b.windows, r)
	h.b.mu.Unlock()
	return r, nil
}

type fakeRuntime struct {
	host      *fakeHost
	open      atomic.Bool
	releases  atomic.Int32
	entered   chan struct{}
	drain     chan struct{}
	uncertain bool
	target    string
	mu        sync.Mutex
	inputs    []Input
}

func (r *fakeRuntime) Target() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.target
}
func (r *fakeRuntime) Alive() bool                            { return r.open.Load() && r.host.live.Load() }
func (r *fakeRuntime) Close() error                           { r.open.Store(false); return nil }
func (r *fakeRuntime) Release(context.Context) error          { r.releases.Add(1); return nil }
func (r *fakeRuntime) Stream(context.Context) (Stream, error) { return nil, ErrUnavailable }
func (r *fakeRuntime) Command(ctx context.Context, c Command) (json.RawMessage, error) {
	if c.Kind == "input" && c.Input != nil {
		r.mu.Lock()
		r.inputs = append(r.inputs, *c.Input)
		r.mu.Unlock()
		return nil, nil
	}
	if c.Kind == "tabs" {
		return json.RawMessage(`[{"id":"tab-1","url":"https://example.test/account?token=secret#x"}]`), nil
	}
	if r.entered != nil {
		close(r.entered)
		select {
		case <-r.drain:
		case <-ctx.Done():
			return nil, ErrUncertain
		}
	}
	if r.uncertain {
		return nil, ErrUncertain
	}
	if c.Kind == "select_tab" {
		r.mu.Lock()
		r.target = c.Target
		r.mu.Unlock()
	}
	return json.RawMessage(`{"authenticated":true}`), nil
}

var human = Owner{Kind: "human", ID: "device/viewer"}
var agent = Owner{Kind: "agent", ID: "task/attachment"}

func newManager(t *testing.T) (*Manager, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{}
	m, err := New(t.TempDir(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	virtualScreen(t, m, true)
	return m, b
}

// setup is a manager whose agent has asked for control once: its window is
// open, and id is that window's.
func setup(t *testing.T) (*Manager, *fakeBackend, string) {
	t.Helper()
	m, b := newManager(t)
	r := must(t, m, agent, Request{Action: "control"})
	must(t, m, agent, Request{Action: "release"})
	id := windowID(t, m, workerOf(agent))
	if r.Lease == nil || b.starts.Load() != 1 || b.opened() != 1 {
		t.Fatalf("lease %v, starts %d, windows %d", r.Lease, b.starts.Load(), b.opened())
	}
	return m, b, id
}
func windowID(t *testing.T, m *Manager, worker string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.byWorker[worker]
	if e == nil {
		t.Fatalf("no window for %s", worker)
	}
	return e.resource.ID
}
func must(t *testing.T, m *Manager, o Owner, q Request) Response {
	t.Helper()
	r, e := m.Handle(context.Background(), o, q)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func virtualScreen(t *testing.T, m *Manager, on bool) {
	t.Helper()
	r := must(t, m, human, Request{Action: "settings", Enabled: &on})
	if r.VirtualScreen == nil || *r.VirtualScreen != on {
		t.Fatalf("settings = %+v", r.VirtualScreen)
	}
}

// Agents that ask for control together get a window each on one browser,
// started once; the ones that arrive while it starts wait for it.
func TestConcurrentFirstControlsShareOneBrowserAndEachGetsAWindow(t *testing.T) {
	m, b := newManager(t)
	b.startGate = make(chan struct{})
	var wg sync.WaitGroup
	leases := make([]*Lease, 5)
	for i := range leases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o := Owner{Kind: "agent", ID: fmt.Sprintf("worker-%d/client", i)}
			r, err := m.Handle(context.Background(), o, Request{Action: "control"})
			if err != nil {
				t.Errorf("agent %d: %v", i, err)
				return
			}
			leases[i] = r.Lease
		}()
	}
	waitUntil(t, "the browser to start", time.Second, func() bool { return b.starts.Load() == 1 })
	time.Sleep(30 * time.Millisecond) // the others are queued behind it
	close(b.startGate)
	wg.Wait()
	if b.starts.Load() != 1 || b.opened() != 5 {
		t.Fatalf("starts %d, windows %d", b.starts.Load(), b.opened())
	}
	seen := map[string]bool{}
	for _, l := range leases {
		if l == nil || seen[l.Target] {
			t.Fatalf("leases %+v", leases)
		}
		seen[l.Target] = true
	}
	listed := must(t, m, human, Request{Action: "list"})
	if len(listed.Resources) != 5 {
		t.Fatalf("Screen lists %d rows", len(listed.Resources))
	}
}

// A browser that cannot start says why, to the agent that asked and the next
// one, and nothing is left stuck afterwards.
func TestStartFailureNamesItsCauseAndLeavesNothingStuck(t *testing.T) {
	m, b := newManager(t)
	b.startErr = errors.New("Private graphical session (kwin_wayland) could not start: no wayland socket")
	for i := 0; i < 2; i++ {
		_, err := m.Handle(context.Background(), agent, Request{Action: "control"})
		if err == nil || !strings.Contains(err.Error(), "no wayland socket") {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	b.startErr = nil
	if r := must(t, m, agent, Request{Action: "control"}); r.Lease == nil {
		t.Fatal("no lease once the cause is gone")
	}
}

// Off, nothing starts for an agent and it is told so.
func TestControlRefusalsNameTheirCause(t *testing.T) {
	m, b := newManager(t)
	virtualScreen(t, m, false)
	_, err := m.Handle(context.Background(), agent, Request{Action: "control"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "virtual screen is off") || b.starts.Load() != 0 {
		t.Fatalf("err = %v, starts %d", err, b.starts.Load())
	}
}

func TestOnlyThePersonSetsTheSwitchAndManagesNothingElse(t *testing.T) {
	m, _, id := setup(t)
	if _, err := m.Handle(context.Background(), agent, Request{Action: "settings", Enabled: new(bool)}); err == nil {
		t.Fatal("an agent changed the setting")
	}
	for _, action := range []string{"create", "start", "stop", "delete", "grant"} {
		if _, err := m.Handle(context.Background(), human, Request{Action: action, ID: id}); err == nil {
			t.Fatalf("%s is still an operation", action)
		}
	}
	if _, err := m.Handle(context.Background(), human, Request{Action: "status", ID: "../../personal"}); err == nil {
		t.Fatal("path traversal")
	}
}

func TestTakeoverDrainsActionAndSuppressesRead(t *testing.T) {
	m, b, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	w := b.last()
	w.entered = make(chan struct{})
	w.drain = make(chan struct{})
	read := make(chan error, 1)
	go func() {
		_, e := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "snapshot"}})
		read <- e
	}()
	<-w.entered
	takeover := make(chan error, 1)
	go func() {
		_, e := m.Handle(context.Background(), human, Request{Action: "control", ID: id})
		takeover <- e
	}()
	e, _ := m.get(id)
	deadline := time.Now().Add(time.Second)
	for {
		e.mu.Lock()
		pending := e.transferring
		e.mu.Unlock()
		if pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("takeover not fenced")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-takeover:
		t.Fatal("takeover acknowledged before drain")
	default:
	}
	close(w.drain)
	if err := <-read; !errors.Is(err, ErrLease) {
		t.Fatalf("read was delivered: %v", err)
	}
	if err := <-takeover; err != nil {
		t.Fatal(err)
	}
	w.entered = nil
}

// A window whose action could not be drained is opened again for the agent
// that holds it or the next one, without touching the browser or the other
// agents' windows; the person cannot take it over meanwhile.
func TestUncertainWindowIsReopenedAndTheBrowserLeftAlone(t *testing.T) {
	m, b, id := setup(t)
	bystander := Owner{Kind: "agent", ID: "bystander/client"}
	bystanderLease := must(t, m, bystander, Request{Action: "control"}).Lease
	first := must(t, m, agent, Request{Action: "control"})
	b.windows[0].uncertain = true
	_, err := m.Handle(context.Background(), agent, Request{Action: "command", Lease: first.Lease, Command: &Command{Kind: "snapshot"}})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), human, Request{Action: "control", ID: id}); err == nil {
		t.Fatal("undrained action admitted takeover")
	}
	again := must(t, m, agent, Request{Action: "control"})
	if again.Lease == nil || again.Lease.Generation == first.Lease.Generation || again.Lease.Epoch == first.Lease.Epoch {
		t.Fatalf("lease %+v after %+v", again.Lease, first.Lease)
	}
	if b.starts.Load() != 1 || b.opened() != 3 || !b.windows[1].Alive() {
		t.Fatalf("starts %d, windows %d", b.starts.Load(), b.opened())
	}
	if b.windows[0].Alive() {
		t.Fatal("the stuck window stayed open")
	}
	if _, err := m.Handle(context.Background(), bystander, Request{Action: "command", Lease: bystanderLease, Command: &Command{Kind: "snapshot"}}); err != nil {
		t.Fatalf("another agent's window was disturbed: %v", err)
	}

	// The person holding an uncertain window is not restarted from under them.
	must(t, m, agent, Request{Action: "release"})
	must(t, m, human, Request{Action: "control", ID: id})
	e, _ := m.get(id)
	e.mu.Lock()
	e.uncertain = true
	e.mu.Unlock()
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control"}); err == nil || b.opened() != 3 {
		t.Fatalf("restarted under the person: err %v, windows %d", err, b.opened())
	}
}

// When the browser itself is gone, each agent's next request opens a window
// on a fresh one, started once.
func TestBrowserLostIsStartedAgainOnce(t *testing.T) {
	m, b := newManager(t)
	other := Owner{Kind: "agent", ID: "other/client"}
	must(t, m, agent, Request{Action: "control"})
	must(t, m, other, Request{Action: "control"})
	b.host.live.Store(false)
	if rows := must(t, m, human, Request{Action: "list"}).Resources; rows[0].State != "stopped" || rows[1].State != "stopped" {
		t.Fatalf("rows = %+v", rows)
	}
	a := must(t, m, agent, Request{Action: "control"})
	o := must(t, m, other, Request{Action: "control"})
	if a.Lease == nil || o.Lease == nil || b.starts.Load() != 2 || !b.hostAlive() {
		t.Fatalf("starts %d", b.starts.Load())
	}
}

// A Worker that ended loses its window; the browser stops with the last one
// and starts again for the next agent.
func TestWindowsFollowTheirWorkersAndTheBrowserStopsWithTheLast(t *testing.T) {
	m, b := newManager(t)
	other := Owner{Kind: "agent", ID: "other/client"}
	must(t, m, agent, Request{Action: "control"})
	must(t, m, other, Request{Action: "control"})
	m.Forget("task")
	if b.windows[0].Alive() || !b.windows[1].Alive() || !b.hostAlive() {
		t.Fatal("closing one agent's window disturbed the other")
	}
	if rows := must(t, m, human, Request{Action: "list"}).Resources; len(rows) != 1 || rows[0].Agent != "other" {
		t.Fatalf("rows = %+v", rows)
	}
	m.Forget("other")
	waitUntil(t, "the browser to stop", time.Second, func() bool { return !b.hostAlive() })
	if r := must(t, m, agent, Request{Action: "control"}); r.Lease == nil || b.starts.Load() != 2 {
		t.Fatalf("starts %d", b.starts.Load())
	}
}

func TestSweepClosesWindowsOfWorkersThatEnded(t *testing.T) {
	previous := sweepEvery
	sweepEvery = 10 * time.Millisecond
	t.Cleanup(func() { sweepEvery = previous })
	m, b := newManager(t)
	var live atomic.Bool
	live.Store(true)
	m.SetWorkerLive(func(w string) bool { return w != "task" || live.Load() })
	must(t, m, agent, Request{Action: "control"})
	time.Sleep(60 * time.Millisecond)
	if !b.windows[0].Alive() {
		t.Fatal("a live Worker lost its window")
	}
	live.Store(false)
	waitUntil(t, "the window to close", time.Second, func() bool { return !b.windows[0].Alive() })
}

// A person signing in on a window keeps it, whatever happens to the Worker.
func TestSweepLeavesAWindowThePersonHolds(t *testing.T) {
	previous := sweepEvery
	sweepEvery = 10 * time.Millisecond
	t.Cleanup(func() { sweepEvery = previous })
	m, b, id := setup(t)
	m.SetWorkerLive(func(string) bool { return false })
	must(t, m, human, Request{Action: "control", ID: id})
	time.Sleep(80 * time.Millisecond)
	if !b.windows[0].Alive() {
		t.Fatal("the window under the person closed")
	}
	must(t, m, human, Request{Action: "release", ID: id})
	waitUntil(t, "the window to close once released", time.Second, func() bool { return !b.windows[0].Alive() })
}

// The profile the person made in the old multi-browser feature becomes the
// agent profile with its data; the others go.
func TestAdoptOldProfileKeepsTheOldestAndItsData(t *testing.T) {
	root := t.TempDir()
	write := func(id, created string) {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(filepath.Join(dir, "profile"), 0700); err != nil {
			t.Fatal(err)
		}
		meta := fmt.Sprintf(`{"id":%q,"name":"x","created_at":%q}`, id, created)
		if err := os.WriteFile(filepath.Join(dir, "resource.json"), []byte(meta), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profile", "Cookies"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old, newer := "5821d748-645f-48f5-8bd9-c1b53c32416a", "5f9301f7-bf25-4902-948c-a97fb818551a"
	write(newer, "2026-11-08T06:49:48Z")
	write(old, "2026-10-03T15:53:05Z")
	for i := 0; i < 2; i++ { // the second start finds nothing left to adopt
		m, err := New(root, &fakeBackend{})
		if err != nil {
			t.Fatal(err)
		}
		m.Close()
		got, err := os.ReadFile(filepath.Join(root, agentProfile, "profile", "Cookies"))
		if err != nil || string(got) != old {
			t.Fatalf("agent profile = %q, %v", got, err)
		}
		for _, id := range []string{old, newer} {
			if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
				t.Fatalf("%s is still there", id)
			}
		}
		if _, err := os.Stat(filepath.Join(root, agentProfile, "resource.json")); !os.IsNotExist(err) {
			t.Fatal("old metadata kept")
		}
	}
}

func TestRejectSymlinkParent(t *testing.T) {
	link := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(filepath.Join(link, "child")); err == nil {
		t.Fatal("followed directory symlink")
	}
}

func TestTargetFencing(t *testing.T) {
	m, _, _ := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	newer := must(t, m, agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "select_tab", Target: "tab-2"}})
	if newer.Lease.Target != "tab-2" {
		t.Fatal("target not updated")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrLease) {
		t.Fatal("stale target accepted")
	}
}

func TestAgentWindowIsFoundByWorkerNotByAnId(t *testing.T) {
	m, _, id := setup(t)
	// Whatever id a long-running tool still carries, the Worker's own window answers.
	r := must(t, m, agent, Request{Action: "control", ID: "5821d748-645f-48f5-8bd9-c1b53c32416a"})
	if r.Resource == nil || r.Resource.ID != id || r.Resource.Agent != "task" {
		t.Fatalf("resource = %+v", r.Resource)
	}
	if _, err := m.Handle(context.Background(), Owner{Kind: "agent", ID: "nobody/client"}, Request{Action: "command", Lease: r.Lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrLease) {
		t.Fatalf("a Worker without a window acted: %v", err)
	}
}

func shortLeases(t *testing.T, d time.Duration) {
	t.Helper()
	previous := leaseLifetime
	leaseLifetime = d
	t.Cleanup(func() { leaseLifetime = previous })
}

func controlOf(t *testing.T, m *Manager, id string) string {
	t.Helper()
	return must(t, m, human, Request{Action: "status", ID: id}).Resource.Control
}

func waitUntil(t *testing.T, what string, within time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A lease ends when its time runs out, on its own timer, and a renewal moves
// that time.
func TestLeaseExpiresOnItsOwnTimerAndRenewalsMoveIt(t *testing.T) {
	shortLeases(t, 150*time.Millisecond)
	m, b, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	if controlOf(t, m, id) != "agent" {
		t.Fatalf("control = %q", controlOf(t, m, id))
	}
	releasesBefore := b.windows[0].releases.Load()

	// Heartbeats keep it past several lifetimes.
	until := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		must(t, m, agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "heartbeat"}})
		time.Sleep(40 * time.Millisecond)
	}
	if controlOf(t, m, id) != "agent" {
		t.Fatal("a lease with heartbeats expired")
	}

	// Without them it ends by itself.
	waitUntil(t, "the lease to expire", 2*time.Second, func() bool { return controlOf(t, m, id) == "idle" })
	if b.windows[0].releases.Load() <= releasesBefore {
		t.Fatal("the runtime was not told to release")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "heartbeat"}}); !errors.Is(err, ErrLease) {
		t.Fatalf("an expired lease was accepted: %v", err)
	}
	must(t, m, agent, Request{Action: "control"})
	waitUntil(t, "the second lease to expire", 2*time.Second, func() bool { return controlOf(t, m, id) == "idle" })
}

func TestExpiryTimersStopWithTheManager(t *testing.T) {
	shortLeases(t, 50*time.Millisecond)
	m, b, id := setup(t)
	must(t, m, human, Request{Action: "control", ID: id})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	releases := b.windows[0].releases.Load()
	time.Sleep(200 * time.Millisecond)
	if b.windows[0].releases.Load() != releases {
		t.Fatal("a lease timer fired after the manager closed")
	}
	if b.hostAlive() {
		t.Fatal("the browser outlived the manager")
	}
}

func humanTouch(t *testing.T, m *Manager, id string, i Input) error {
	t.Helper()
	_, err := m.Handle(context.Background(), human, Request{Action: "touch", ID: id, Command: &Command{Kind: "input", Input: &i}})
	return err
}

// The person's clicks and typing reach the page while the agent keeps the
// browser; nothing pauses it. It is told what happened, never the text.
func TestPersonInputDoesNotPauseAgentAndIsTold(t *testing.T) {
	m, b, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	for _, i := range []Input{{Kind: "mouseMoved", X: 5, Y: 5}, {Kind: "mousePressed", X: 10, Y: 20, Button: "left"}, {Kind: "mouseReleased", X: 10, Y: 20, Button: "left"}, {Kind: "text", Text: "hunter2!"}} {
		if err := humanTouch(t, m, id, i); err != nil {
			t.Fatal(err)
		}
	}
	w := b.windows[0]
	w.mu.Lock()
	delivered := len(w.inputs)
	w.mu.Unlock()
	if delivered != 4 {
		t.Fatalf("delivered %d inputs", delivered)
	}
	if controlOf(t, m, id) != "agent" {
		t.Fatal("the person's input moved control away from the agent")
	}
	r := must(t, m, agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "snapshot"}})
	notes := strings.Join(r.Notes, "\n")
	if !strings.Contains(notes, "1 click") || !strings.Contains(notes, "8 typed characters") || strings.Contains(notes, "hunter2") {
		t.Fatalf("notes = %q", notes)
	}
	if r2 := must(t, m, agent, Request{Action: "command", Lease: r.Lease, Command: &Command{Kind: "snapshot"}}); len(r2.Notes) != 0 {
		t.Fatalf("told twice: %q", r2.Notes)
	}
}

// Input that lands while the agent's own action runs is flagged as such.
func TestPersonInputDuringAgentActionIsFlagged(t *testing.T) {
	m, b, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	w := b.windows[0]
	w.entered = make(chan struct{})
	w.drain = make(chan struct{})
	done := make(chan Response, 1)
	go func() {
		r, _ := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "click", Ref: "@e1"}})
		done <- r
	}()
	<-w.entered
	if err := humanTouch(t, m, id, Input{Kind: "mousePressed", X: 1, Y: 1, Button: "left"}); err != nil {
		t.Fatal(err)
	}
	close(w.drain)
	r := <-done
	if !strings.Contains(strings.Join(r.Notes, " "), "while your own action was running") || !strings.Contains(strings.Join(r.Notes, " "), "https://example.test/account.") {
		t.Fatalf("notes = %q", r.Notes)
	}
	w.entered = nil
}

// Take over is hard: the agent waits and is told; handing back tells it what
// the person did.
func TestTakeOverMakesAgentWaitAndHandBackTellsIt(t *testing.T) {
	m, _, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	taken := must(t, m, human, Request{Action: "control", ID: id})
	if taken.Resource.Agent != workerOf(agent) {
		t.Fatalf("the row lost its agent while the person holds it: %q", taken.Resource.Agent)
	}
	listed := must(t, m, human, Request{Action: "list"})
	if len(listed.Resources) != 1 || listed.Resources[0].Agent != workerOf(agent) || listed.Resources[0].Control != "human" {
		t.Fatalf("list = %+v", listed.Resources)
	}
	_, err := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "snapshot"}})
	if !errors.Is(err, ErrLease) || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("err = %v", err)
	}
	_, err = m.Handle(context.Background(), agent, Request{Action: "control"})
	if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "do not poll") {
		t.Fatalf("err = %v", err)
	}
	must(t, m, human, Request{Action: "release", ID: id})
	r := must(t, m, agent, Request{Action: "control"})
	if !strings.Contains(strings.Join(r.Notes, " "), "handed it back") {
		t.Fatalf("notes = %q", r.Notes)
	}
}

// Stop ends the agent's control and keeps that Worker out until the person
// hands the window back; other Workers have windows of their own.
func TestStopEndsAgentControlUntilHandBack(t *testing.T) {
	m, _, id := setup(t)
	lease := must(t, m, agent, Request{Action: "control"}).Lease
	if got := must(t, m, human, Request{Action: "status", ID: id}).Resource.Agent; got != "task" {
		t.Fatalf("agent = %q", got)
	}
	must(t, m, human, Request{Action: "halt", ID: id})
	if controlOf(t, m, id) != "idle" {
		t.Fatal("control not ended")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", Lease: lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	other := Owner{Kind: "agent", ID: "other-task/attachment"}
	must(t, m, other, Request{Action: "control"})
	must(t, m, other, Request{Action: "release"})
	must(t, m, human, Request{Action: "control", ID: id})
	must(t, m, human, Request{Action: "release", ID: id})
	must(t, m, agent, Request{Action: "control"})
}

// Recovering a stuck window while the Screen lists and the sweeper looks must
// not deadlock on the manager and entry locks; it once froze every agent.
func TestRecoveringAWindowWhileListingAndSweepingDoesNotDeadlock(t *testing.T) {
	previous := sweepEvery
	sweepEvery = time.Millisecond
	t.Cleanup(func() { sweepEvery = previous })
	m, b := newManager(t)
	m.SetWorkerLive(func(string) bool { return true })
	other := Owner{Kind: "agent", ID: "other/client"}
	must(t, m, other, Request{Action: "control"})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					m.Handle(context.Background(), human, Request{Action: "list"})
					m.VirtualScreen()
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			r, err := m.Handle(context.Background(), agent, Request{Action: "control"})
			if err != nil {
				t.Error(err)
				return
			}
			b.last().uncertain = false
			e, _ := m.get(windowID(t, m, "task"))
			e.mu.Lock()
			e.uncertain = true
			e.mu.Unlock()
			_ = r
			m.Handle(context.Background(), agent, Request{Action: "release"})
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock: recovery, list and sweep did not finish")
	}
	close(stop)
	wg.Wait()
}
