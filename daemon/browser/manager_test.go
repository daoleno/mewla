package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if err := m.ValidateAttachment(ctx, id); err == nil {
		t.Fatal("ungranted stopped profile selected")
	}
	must(t, m, human, Request{Action: "grant", ID: id, AllowAgents: true})
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
