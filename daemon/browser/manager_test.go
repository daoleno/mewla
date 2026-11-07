package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	starts  atomic.Int32
	runtime *fakeRuntime
}

func (b *fakeBackend) Capability(context.Context) Capability { return Capability{Available: true} }
func (b *fakeBackend) Start(context.Context, string) (Runtime, error) {
	b.starts.Add(1)
	b.runtime.live.Store(true)
	return b.runtime, nil
}

type fakeRuntime struct {
	live      atomic.Bool
	releases  atomic.Int32
	entered   chan struct{}
	drain     chan struct{}
	uncertain bool
	target    string
	mu        sync.Mutex
	inputs    []Input
}

func (r *fakeRuntime) Target() string {
	if r.target != "" {
		return r.target
	}
	return "tab-1"
}
func (r *fakeRuntime) Alive() bool                            { return r.live.Load() }
func (r *fakeRuntime) Close() error                           { r.live.Store(false); return nil }
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
		r.target = c.Target
	}
	return json.RawMessage(`{"authenticated":true}`), nil
}

var human = Owner{Kind: "human", ID: "device/viewer"}
var agent = Owner{Kind: "agent", ID: "task/attachment"}

func setup(t *testing.T) (*Manager, *fakeBackend, string) {
	t.Helper()
	b := &fakeBackend{runtime: &fakeRuntime{}}
	m, err := New(t.TempDir(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	r, err := m.Handle(context.Background(), human, Request{Action: "create", Name: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	return m, b, r.Resource.ID
}
func must(t *testing.T, m *Manager, o Owner, q Request) Response {
	t.Helper()
	r, e := m.Handle(context.Background(), o, q)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestBrowserConcurrentLaunchAndTaskIndependence(t *testing.T) {
	m, b, id := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Handle(context.Background(), human, Request{Action: "start", ID: id})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if b.starts.Load() != 1 {
		t.Fatal("multiple browser processes")
	}
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: true})
	first := must(t, m, agent, Request{Action: "control", ID: id})
	must(t, m, agent, Request{Action: "release", ID: id})
	second := must(t, m, agent, Request{Action: "control", ID: id})
	if first.Lease.Generation != second.Lease.Generation || !b.runtime.Alive() {
		t.Fatal("task detach destroyed browser")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: first.Lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrLease) {
		t.Fatal("old epoch accepted")
	}
	must(t, m, human, Request{Action: "control", ID: id})
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control", ID: id}); !errors.Is(err, ErrBusy) {
		t.Fatal("agent displaced human")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: second.Lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrLease) {
		t.Fatal("agent read human login")
	}
	m.ReleaseOwner(human)
	if b.runtime.releases.Load() < 3 {
		t.Fatal("held input not released")
	}
}
func TestTakeoverDrainsActionAndSuppressesRead(t *testing.T) {
	m, b, id := setup(t)
	must(t, m, human, Request{Action: "start", ID: id})
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: true})
	lease := must(t, m, agent, Request{Action: "control", ID: id}).Lease
	b.runtime.entered = make(chan struct{})
	b.runtime.drain = make(chan struct{})
	read := make(chan error, 1)
	go func() {
		_, e := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}})
		read <- e
	}()
	<-b.runtime.entered
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
	close(b.runtime.drain)
	if err := <-read; !errors.Is(err, ErrLease) {
		t.Fatalf("read was delivered: %v", err)
	}
	if err := <-takeover; err != nil {
		t.Fatal(err)
	}
	b.runtime.entered = nil
}
func TestUncertainActionRequiresRestart(t *testing.T) {
	m, b, id := setup(t)
	must(t, m, human, Request{Action: "start", ID: id})
	lease := must(t, m, human, Request{Action: "control", ID: id}).Lease
	b.runtime.uncertain = true
	_, err := m.Handle(context.Background(), human, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), human, Request{Action: "control", ID: id}); err == nil {
		t.Fatal("undrained action admitted takeover")
	}
	must(t, m, human, Request{Action: "stop", ID: id})
	must(t, m, human, Request{Action: "start", ID: id})
	fresh := must(t, m, human, Request{Action: "control", ID: id}).Lease
	if fresh.Generation == lease.Generation {
		t.Fatal("restart reused process identity")
	}
}
func TestProfilesPathsAndDeletion(t *testing.T) {
	m, _, id := setup(t)
	for _, action := range []string{"start", "delete", "grant"} {
		if _, err := m.Handle(context.Background(), human, Request{Action: action, ID: "../../personal"}); err == nil {
			t.Fatal("path traversal")
		}
		if _, err := m.Handle(context.Background(), agent, Request{Action: action, ID: id}); err == nil {
			t.Fatal("agent managed resource")
		}
	}
	profile := filepath.Join(m.root, id, "profile")
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "fixture"), []byte("persistent"), 0600); err != nil {
		t.Fatal(err)
	}
	must(t, m, human, Request{Action: "stop", ID: id})
	m.Close()
	reload, err := New(m.root, m.backend)
	if err != nil {
		t.Fatal(err)
	}
	defer reload.Close()
	if _, err := os.Stat(filepath.Join(profile, "fixture")); err != nil {
		t.Fatal("stop lost profile")
	}
	must(t, reload, human, Request{Action: "delete", ID: id})
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("explicit delete retained data")
	}
}
func TestRejectSymlinkMetadataAndParent(t *testing.T) {
	m, _, id := setup(t)
	m.Close()
	meta := filepath.Join(m.root, id, "resource.json")
	saved := meta + ".saved"
	if err := os.Rename(meta, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(saved, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := New(m.root, m.backend); err == nil {
		t.Fatal("followed metadata symlink")
	}
	link := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(filepath.Join(link, "child")); err == nil {
		t.Fatal("followed directory symlink")
	}
}
func TestGrantRevocationAndTargetFencing(t *testing.T) {
	m, _, id := setup(t)
	must(t, m, human, Request{Action: "start", ID: id})
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: true})
	lease := must(t, m, agent, Request{Action: "control", ID: id}).Lease
	newer := must(t, m, agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "select_tab", Target: "tab-2"}})
	if newer.Lease.Target != "tab-2" {
		t.Fatal("target not updated")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrLease) {
		t.Fatal("stale target accepted")
	}
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: false})
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control", ID: id}); !errors.Is(err, ErrBusy) {
		t.Fatal("grant revocation bypassed")
	}
}

func TestAttachmentSelectionRequiresOpenGrantedResourceWithoutSideEffects(t *testing.T) {
	m, backend, id := setup(t)
	ctx := context.Background()
	if listed := must(t, m, agent, Request{Action: "list"}).Resources; len(listed) != 1 || !listed[0].AllowAgents {
		t.Fatal("new browser is not available to same-server Agents by default")
	}
	if err := m.ValidateAttachment(ctx, id); err == nil {
		t.Fatal("stopped profile selected")
	}
	if backend.starts.Load() != 0 {
		t.Fatal("selection implicitly launched browser")
	}
	must(t, m, human, Request{Action: "start", ID: id})
	if err := m.ValidateAttachment(ctx, id); err != nil {
		t.Fatal(err)
	}
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: false})
	if err := m.ValidateAttachment(ctx, id); err == nil {
		t.Fatal("revoked grant selected")
	}
	if !backend.runtime.Alive() {
		t.Fatal("selection or revocation closed user browser")
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
	must(t, m, human, Request{Action: "start", ID: id})
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: true})
	lease := must(t, m, agent, Request{Action: "control", ID: id}).Lease
	if controlOf(t, m, id) != "agent" {
		t.Fatalf("control = %q", controlOf(t, m, id))
	}
	releasesBefore := b.runtime.releases.Load()

	// Heartbeats keep it past several lifetimes.
	until := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		must(t, m, agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "heartbeat"}})
		time.Sleep(40 * time.Millisecond)
	}
	if controlOf(t, m, id) != "agent" {
		t.Fatal("a lease with heartbeats expired")
	}

	// Without them it ends by itself.
	waitUntil(t, "the lease to expire", 2*time.Second, func() bool { return controlOf(t, m, id) == "idle" })
	if b.runtime.releases.Load() <= releasesBefore {
		t.Fatal("the runtime was not told to release")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "heartbeat"}}); !errors.Is(err, ErrLease) {
		t.Fatalf("an expired lease was accepted: %v", err)
	}
	// The entry can be controlled again, and that lease expires too.
	must(t, m, agent, Request{Action: "control", ID: id})
	waitUntil(t, "the second lease to expire", 2*time.Second, func() bool { return controlOf(t, m, id) == "idle" })
}

func TestExpiryTimersStopWithTheManager(t *testing.T) {
	shortLeases(t, 50*time.Millisecond)
	m, b, id := setup(t)
	must(t, m, human, Request{Action: "start", ID: id})
	must(t, m, human, Request{Action: "control", ID: id})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	releases := b.runtime.releases.Load()
	time.Sleep(200 * time.Millisecond)
	if b.runtime.releases.Load() != releases {
		t.Fatal("a lease timer fired after the manager closed")
	}
}

func humanTouch(t *testing.T, m *Manager, id string, i Input) error {
	t.Helper()
	_, err := m.Handle(context.Background(), human, Request{Action: "touch", ID: id, Command: &Command{Kind: "input", Input: &i}})
	return err
}

func agentReady(t *testing.T, m *Manager, id string) *Lease {
	t.Helper()
	must(t, m, human, Request{Action: "start", ID: id})
	return must(t, m, agent, Request{Action: "control", ID: id}).Lease
}

// The person's clicks and typing reach the page while the agent keeps the
// browser; nothing pauses it. It is told what happened, never the text.
func TestPersonInputDoesNotPauseAgentAndIsTold(t *testing.T) {
	m, b, id := setup(t)
	lease := agentReady(t, m, id)
	for _, i := range []Input{{Kind: "mouseMoved", X: 5, Y: 5}, {Kind: "mousePressed", X: 10, Y: 20, Button: "left"}, {Kind: "mouseReleased", X: 10, Y: 20, Button: "left"}, {Kind: "text", Text: "hunter2!"}} {
		if err := humanTouch(t, m, id, i); err != nil {
			t.Fatal(err)
		}
	}
	b.runtime.mu.Lock()
	delivered := len(b.runtime.inputs)
	b.runtime.mu.Unlock()
	if delivered != 4 {
		t.Fatalf("delivered %d inputs", delivered)
	}
	if controlOf(t, m, id) != "agent" {
		t.Fatal("the person's input moved control away from the agent")
	}
	r := must(t, m, agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}})
	notes := strings.Join(r.Notes, "\n")
	if !strings.Contains(notes, "1 click") || !strings.Contains(notes, "8 typed characters") || strings.Contains(notes, "hunter2") {
		t.Fatalf("notes = %q", notes)
	}
	if r2 := must(t, m, agent, Request{Action: "command", ID: id, Lease: r.Lease, Command: &Command{Kind: "snapshot"}}); len(r2.Notes) != 0 {
		t.Fatalf("told twice: %q", r2.Notes)
	}
}

// Input that lands while the agent's own action runs is flagged as such.
func TestPersonInputDuringAgentActionIsFlagged(t *testing.T) {
	m, b, id := setup(t)
	lease := agentReady(t, m, id)
	b.runtime.entered = make(chan struct{})
	b.runtime.drain = make(chan struct{})
	done := make(chan Response, 1)
	go func() {
		r, _ := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "click", Ref: "@e1"}})
		done <- r
	}()
	<-b.runtime.entered
	if err := humanTouch(t, m, id, Input{Kind: "mousePressed", X: 1, Y: 1, Button: "left"}); err != nil {
		t.Fatal(err)
	}
	close(b.runtime.drain)
	r := <-done
	if !strings.Contains(strings.Join(r.Notes, " "), "while your own action was running") || !strings.Contains(strings.Join(r.Notes, " "), "https://example.test/account.") {
		t.Fatalf("notes = %q", r.Notes)
	}
	b.runtime.entered = nil
}

// Take over is hard: the agent waits and is told; handing back tells it what
// the person did.
func TestTakeOverMakesAgentWaitAndHandBackTellsIt(t *testing.T) {
	m, _, id := setup(t)
	lease := agentReady(t, m, id)
	must(t, m, human, Request{Action: "control", ID: id})
	_, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}})
	if !errors.Is(err, ErrLease) || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("err = %v", err)
	}
	_, err = m.Handle(context.Background(), agent, Request{Action: "control", ID: id})
	if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "do not poll") {
		t.Fatalf("err = %v", err)
	}
	must(t, m, human, Request{Action: "release", ID: id})
	r := must(t, m, agent, Request{Action: "control", ID: id})
	if !strings.Contains(strings.Join(r.Notes, " "), "handed it back") {
		t.Fatalf("notes = %q", r.Notes)
	}
}

// Stop ends the agent's control and keeps that Worker out until the person
// hands the browser back.
func TestStopEndsAgentControlUntilHandBack(t *testing.T) {
	m, _, id := setup(t)
	lease := agentReady(t, m, id)
	if got := must(t, m, human, Request{Action: "status", ID: id}).Resource.Agent; got != "task" {
		t.Fatalf("agent = %q", got)
	}
	must(t, m, human, Request{Action: "halt", ID: id})
	if controlOf(t, m, id) != "idle" {
		t.Fatal("control not ended")
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "command", ID: id, Lease: lease, Command: &Command{Kind: "snapshot"}}); !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control", ID: id}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	other := Owner{Kind: "agent", ID: "other-task/attachment"}
	must(t, m, other, Request{Action: "control", ID: id})
	must(t, m, other, Request{Action: "release", ID: id})
	must(t, m, human, Request{Action: "control", ID: id})
	must(t, m, human, Request{Action: "release", ID: id})
	must(t, m, agent, Request{Action: "control", ID: id})
}

func virtualScreen(t *testing.T, m *Manager, on bool) {
	t.Helper()
	r := must(t, m, human, Request{Action: "settings", Enabled: &on})
	if r.VirtualScreen == nil || *r.VirtualScreen != on {
		t.Fatalf("settings = %+v", r.VirtualScreen)
	}
}

// With the virtual screen on, a Worker is handed a browser no agent holds,
// and a new one when they are all taken; off, nothing is attached. The
// choice survives a restart and only the person can change it.
func TestVirtualScreenAutoAttachment(t *testing.T) {
	m, _, id := setup(t)
	ctx := context.Background()
	if got := m.AutoAttachment(ctx); got != "" {
		t.Fatalf("attached with the virtual screen off: %q", got)
	}
	if _, err := m.Handle(ctx, agent, Request{Action: "settings", Enabled: new(bool)}); err == nil {
		t.Fatal("an agent changed the setting")
	}
	virtualScreen(t, m, true)
	first := m.AutoAttachment(ctx)
	if first != id {
		t.Fatalf("first Worker got %q, want the idle browser %q", first, id)
	}
	second := m.AutoAttachment(ctx)
	if second == "" || second == id {
		t.Fatalf("second Worker shared the reserved browser: %q", second)
	}
	listed := must(t, m, human, Request{Action: "list"})
	if len(listed.Resources) != 2 || !listed.Resources[1].AllowAgents || listed.VirtualScreen == nil || !*listed.VirtualScreen {
		t.Fatalf("list = %+v", listed)
	}
	m.Close()
	reload, err := New(m.root, m.backend)
	if err != nil {
		t.Fatal(err)
	}
	defer reload.Close()
	if !reload.VirtualScreen() {
		t.Fatal("the setting was lost on restart")
	}
}

// The browser an agent is attached to starts when it first asks for control,
// not when the Worker launches, and only with the virtual screen on.
func TestAgentControlStartsAttachedBrowserOnlyWithVirtualScreen(t *testing.T) {
	m, b, id := setup(t)
	if _, err := m.Handle(context.Background(), agent, Request{Action: "control", ID: id}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("started without the virtual screen: %v", err)
	}
	virtualScreen(t, m, true)
	r := must(t, m, agent, Request{Action: "control", ID: id})
	if r.Lease == nil || b.starts.Load() != 1 {
		t.Fatalf("lease %v, starts %d", r.Lease, b.starts.Load())
	}
	if err := m.ValidateAttachment(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}
