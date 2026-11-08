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

// fakeBackend stands in for the one browser: Start makes a host, Window
// follows a window of it.
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
func (h *fakeHost) Window(_ context.Context, target string) (Runtime, error) {
	if !h.live.Load() {
		return nil, ErrUnavailable
	}
	r := &fakeRuntime{host: h, target: target}
	r.open.Store(true)
	h.b.mu.Lock()
	h.b.windows = append(h.b.windows, r)
	h.b.mu.Unlock()
	return r, nil
}

type fakeRuntime struct {
	host     *fakeHost
	open     atomic.Bool
	releases atomic.Int32
	target   string
	page     string
	mu       sync.Mutex
	inputs   []Input
}

func (r *fakeRuntime) Target() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.target
}
func (r *fakeRuntime) SetTarget(t string) { r.mu.Lock(); r.target = t; r.mu.Unlock() }
func (r *fakeRuntime) Alive() bool        { return r.open.Load() && r.host.live.Load() }
func (r *fakeRuntime) Close() error       { r.open.Store(false); return nil }
func (r *fakeRuntime) Release(context.Context) error {
	r.releases.Add(1)
	return nil
}
func (r *fakeRuntime) Stream(context.Context) (Stream, error) { return nil, ErrUnavailable }
func (r *fakeRuntime) Page(context.Context) string            { return r.page }
func (r *fakeRuntime) Command(_ context.Context, c Command) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"ran": c.Kind})
}
func (r *fakeRuntime) Input(_ context.Context, i Input) error {
	r.mu.Lock()
	r.inputs = append(r.inputs, i)
	r.mu.Unlock()
	return nil
}

var human = Owner{Kind: "human", ID: "device/viewer"}

func agentOf(worker string) Owner { return Owner{Kind: "agent", ID: worker} }

var agent = agentOf("task")

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

// setup is a manager whose Worker "task" has a window: the extension opened
// it, and id is its row's.
func setup(t *testing.T) (*Manager, *fakeBackend, string) {
	t.Helper()
	m, b := newManager(t)
	open(t, m, "task", "c1", "tab-1")
	if b.starts.Load() != 1 || b.opened() != 1 {
		t.Fatalf("starts %d, windows %d", b.starts.Load(), b.opened())
	}
	return m, b, windowID(t, m, "task")
}
func open(t *testing.T, m *Manager, worker, key, target string) {
	t.Helper()
	if err := m.EnsureHost(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Opened(context.Background(), worker, key, target); err != nil {
		t.Fatal(err)
	}
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
func controlOf(t *testing.T, m *Manager, id string) string {
	t.Helper()
	return must(t, m, human, Request{Action: "status", ID: id}).Resource.Control
}
func shortLeases(t *testing.T, d time.Duration) {
	t.Helper()
	old := leaseLifetime
	leaseLifetime = d
	t.Cleanup(func() { leaseLifetime = old })
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

// Agents that ask for the browser together share one start; the ones that
// arrive while it starts wait for it, and each gets its own row.
func TestConcurrentFirstWindowsShareOneBrowserAndEachGetsARow(t *testing.T) {
	m, b := newManager(t)
	b.startGate = make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.EnsureHost(context.Background()); err != nil {
				t.Errorf("agent %d: %v", i, err)
				return
			}
			if err := m.Opened(context.Background(), fmt.Sprintf("worker-%d", i), fmt.Sprintf("c%d", i), fmt.Sprintf("tab-%d", i)); err != nil {
				t.Errorf("agent %d: %v", i, err)
			}
		}()
	}
	waitUntil(t, "the browser to start", time.Second, func() bool { return b.starts.Load() == 1 })
	time.Sleep(30 * time.Millisecond) // the others are queued behind it
	close(b.startGate)
	wg.Wait()
	if b.starts.Load() != 1 || b.opened() != 5 {
		t.Fatalf("starts %d, windows %d", b.starts.Load(), b.opened())
	}
	listed := must(t, m, human, Request{Action: "list"})
	if len(listed.Resources) != 5 {
		t.Fatalf("Screen lists %d rows", len(listed.Resources))
	}
	for _, r := range listed.Resources {
		if r.Control != "agent" || r.State != "running" || r.Agent == "" || r.Target == "" {
			t.Fatalf("row %+v", r)
		}
	}
}

// A browser that cannot start says why, to the agent that asked and the next
// one, and nothing is left stuck afterwards.
func TestStartFailureNamesItsCauseAndLeavesNothingStuck(t *testing.T) {
	m, b := newManager(t)
	b.startErr = errors.New("Private graphical session (kwin_wayland) could not start: no wayland socket")
	for i := 0; i < 2; i++ {
		err := m.EnsureHost(context.Background())
		if err == nil || !strings.Contains(err.Error(), "no wayland socket") {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	b.startErr = nil
	if err := m.EnsureHost(context.Background()); err != nil {
		t.Fatalf("still failing once the cause is gone: %v", err)
	}
}

func TestOnlyThePersonSetsTheSwitchAndAgentsOnlyBeginAndEnd(t *testing.T) {
	m, _, id := setup(t)
	for _, q := range []Request{{Action: "settings", Enabled: new(bool)}, {Action: "list"}, {Action: "control", ID: id}, {Action: "halt", ID: id}, {Action: "status", ID: id}, {Action: "touch", ID: id}} {
		if _, err := m.Handle(context.Background(), agent, q); err == nil {
			t.Fatalf("an agent was allowed %q", q.Action)
		}
	}
	if !m.VirtualScreen() {
		t.Fatal("an agent changed the setting")
	}
}

// A Worker with no window yet is not refused: its first tool call opens one.
func TestAgentWithoutAWindowIsNotRefused(t *testing.T) {
	m, _ := newManager(t)
	must(t, m, agentOf("fresh"), Request{Action: "begin"})
	if r := must(t, m, agentOf("fresh"), Request{Action: "end"}); len(r.Notes) != 0 {
		t.Fatalf("notes %v", r.Notes)
	}
}

func TestViewedFollowsThePageAndClosedTakesTheRowOff(t *testing.T) {
	m, b, id := setup(t)
	m.Viewed("c1", "tab-9")
	if got := must(t, m, human, Request{Action: "status", ID: id}).Resource.Target; got != "tab-9" || b.windows[0].Target() != "tab-9" {
		t.Fatalf("target %q", got)
	}
	m.Closed("c1")
	if rows := must(t, m, human, Request{Action: "list"}).Resources; len(rows) != 0 {
		t.Fatalf("rows %+v", rows)
	}
	if b.windows[0].Alive() {
		t.Fatal("the window's runtime stayed open")
	}
}

// A Worker that ends takes its window with it, and the extension is asked to
// close it; the browser stops with the last one.
func TestForgetClosesTheWindowAndTheBrowserStopsWithTheLast(t *testing.T) {
	old := hostGrace
	hostGrace = 0
	t.Cleanup(func() { hostGrace = old })
	m, b := newManager(t)
	var closed []string
	m.SetWindowCloser(func(key string) { closed = append(closed, key) })
	open(t, m, "one", "c1", "tab-1")
	open(t, m, "two", "c2", "tab-2")
	m.Forget("one")
	if len(closed) != 1 || closed[0] != "c1" {
		t.Fatalf("closed %v", closed)
	}
	time.Sleep(50 * time.Millisecond)
	if !b.hostAlive() {
		t.Fatal("the browser stopped with a window left")
	}
	m.Forget("two")
	waitUntil(t, "the browser to stop", time.Second, func() bool { return !b.hostAlive() })
}

func TestSweepClosesWindowsOfWorkersThatEnded(t *testing.T) {
	m, _, _ := setup(t)
	var alive atomic.Bool
	alive.Store(true)
	m.SetWorkerLive(func(string) bool { return alive.Load() })
	go m.sweep(10 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if len(must(t, m, human, Request{Action: "list"}).Resources) != 1 {
		t.Fatal("the window of a live Worker was closed")
	}
	alive.Store(false)
	waitUntil(t, "the sweep", time.Second, func() bool { return len(must(t, m, human, Request{Action: "list"}).Resources) == 0 })
}

func TestSweepLeavesAWindowThePersonHolds(t *testing.T) {
	m, _, id := setup(t)
	must(t, m, human, Request{Action: "control", ID: id})
	m.SetWorkerLive(func(string) bool { return false })
	go m.sweep(10 * time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if len(must(t, m, human, Request{Action: "list"}).Resources) != 1 {
		t.Fatal("the person's window was closed")
	}
}

// Take over refuses the agent's next call at once, waits for the one running,
// and hand back tells the agent what happened.
func TestTakeOverWaitsForTheRunningCallRefusesTheNextAndHandBackTellsIt(t *testing.T) {
	m, b, id := setup(t)
	must(t, m, agent, Request{Action: "begin"}) // a tool call is running
	got := make(chan Response, 1)
	go func() { got <- must(t, m, human, Request{Action: "control", ID: id}) }()
	waitUntil(t, "the take over to start", time.Second, func() bool {
		m.mu.Lock()
		e := m.byWorker["task"]
		m.mu.Unlock()
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.transferring
	})
	if _, err := m.Handle(context.Background(), agent, Request{Action: "begin"}); !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("the next call during a take over: %v", err)
	}
	select {
	case <-got:
		t.Fatal("the take over went ahead with a call still running")
	case <-time.After(60 * time.Millisecond):
	}
	must(t, m, agent, Request{Action: "end"})
	r := <-got
	if r.Lease == nil || controlOf(t, m, id) != "human" || b.windows[0].releases.Load() == 0 {
		t.Fatalf("lease %v, control %q", r.Lease, controlOf(t, m, id))
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "begin"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("a call while the person holds it: %v", err)
	}
	// The person's input under their lease reaches the page.
	must(t, m, human, Request{Action: "command", ID: id, Lease: r.Lease, Command: &Command{Kind: "input", Input: &Input{Kind: "text", Text: "hello"}}})
	if len(b.windows[0].inputs) != 1 {
		t.Fatalf("inputs %v", b.windows[0].inputs)
	}
	must(t, m, human, Request{Action: "release", ID: id})
	must(t, m, agent, Request{Action: "begin"})
	notes := must(t, m, agent, Request{Action: "end"}).Notes
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), "handed it back") {
		t.Fatalf("notes %v", notes)
	}
}

// A take over does not wait for a call forever.
func TestTakeOverGoesAheadAfterTheDrainWait(t *testing.T) {
	old := drainWait
	drainWait = 50 * time.Millisecond
	t.Cleanup(func() { drainWait = old })
	m, _, id := setup(t)
	must(t, m, agent, Request{Action: "begin"})
	if r := must(t, m, human, Request{Action: "control", ID: id}); r.Lease == nil {
		t.Fatal("no lease")
	}
}

// The person may use the window without taking it over: the page gets their
// input, nothing stops the agent, and it hears what happened, never the text.
func TestPersonInputDoesNotPauseAgentAndIsTold(t *testing.T) {
	m, b, id := setup(t)
	b.windows[0].page = "https://example.test/account"
	must(t, m, agent, Request{Action: "begin"})
	for _, i := range []Input{{Kind: "mousePressed", X: 3, Y: 4, Button: "left"}, {Kind: "text", Text: "secret"}, {Kind: "mouseWheel", DY: 40}} {
		if _, err := m.Handle(context.Background(), human, Request{Action: "touch", ID: id, Command: &Command{Kind: "input", Input: &i}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(b.windows[0].inputs) != 3 {
		t.Fatalf("inputs %v", b.windows[0].inputs)
	}
	notes := strings.Join(must(t, m, agent, Request{Action: "end"}).Notes, "\n")
	for _, want := range []string{"1 click", "6 typed characters", "1 scroll", "while your own action was running", "https://example.test/account"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("notes lack %q: %s", want, notes)
		}
	}
	if strings.Contains(notes, "secret") {
		t.Fatal("the typed text was told")
	}
	// Told once.
	must(t, m, agent, Request{Action: "begin"})
	if again := must(t, m, agent, Request{Action: "end"}).Notes; len(again) != 0 {
		t.Fatalf("told twice: %v", again)
	}
}

// Stop refuses the Worker until the person hands the browser back.
func TestStopRefusesTheWorkerUntilHandBack(t *testing.T) {
	m, _, id := setup(t)
	must(t, m, human, Request{Action: "halt", ID: id})
	if _, err := m.Handle(context.Background(), agent, Request{Action: "begin"}); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("after Stop: %v", err)
	}
	if _, err := m.Handle(context.Background(), agentOf("other"), Request{Action: "begin"}); err != nil {
		t.Fatalf("another Worker was stopped: %v", err)
	}
	must(t, m, human, Request{Action: "control", ID: id})
	must(t, m, human, Request{Action: "release", ID: id})
	must(t, m, agent, Request{Action: "begin"})
}

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

// A lease ends when its time runs out, on its own timer, and a renewal moves
// that time.
// A lease ends when its time runs out, on its own timer, and a renewal moves
// that time.
func TestLeaseExpiresOnItsOwnTimerAndRenewalsMoveIt(t *testing.T) {
	shortLeases(t, 150*time.Millisecond)
	m, b, id := setup(t)
	lease := must(t, m, human, Request{Action: "control", ID: id}).Lease
	if controlOf(t, m, id) != "human" {
		t.Fatalf("control = %q", controlOf(t, m, id))
	}
	releasesBefore := b.windows[0].releases.Load()
	until := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		must(t, m, human, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "heartbeat"}})
		time.Sleep(40 * time.Millisecond)
	}
	if controlOf(t, m, id) != "human" {
		t.Fatal("a lease with heartbeats expired")
	}
	waitUntil(t, "the lease to expire", 2*time.Second, func() bool { return controlOf(t, m, id) == "agent" })
	if b.windows[0].releases.Load() <= releasesBefore {
		t.Fatal("the runtime was not told to release")
	}
	if _, err := m.Handle(context.Background(), human, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "heartbeat"}}); !errors.Is(err, ErrLease) {
		t.Fatalf("an expired lease was accepted: %v", err)
	}
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

// A list taken while a window is reopened and the sweep runs must not deadlock.
func TestOpeningAWindowAgainWhileListingAndSweepingDoesNotDeadlock(t *testing.T) {
	m, _, _ := setup(t)
	m.SetWorkerLive(func(string) bool { return true })
	go m.sweep(2 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			open(t, m, "task", fmt.Sprintf("c%d", i+2), "tab-x")
			must(t, m, human, Request{Action: "list"})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock")
	}
	if rows := must(t, m, human, Request{Action: "list"}).Resources; len(rows) != 1 {
		t.Fatalf("a Worker has %d rows", len(rows))
	}
}
