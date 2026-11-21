package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/grants"
	"github.com/daoleno/mewla/daemon/screen"
)

type needBackend struct {
	mu    sync.Mutex
	state map[string]grants.State
}

func (b *needBackend) Supported(string) bool { return true }
func (b *needBackend) State(_ context.Context, permission string) grants.State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state[permission]
}
func (b *needBackend) OpenSettings(context.Context, string) error { return nil }

type needRestarter struct{}

func (needRestarter) Quiet() bool    { return true }
func (needRestarter) InPlace() error { return nil }
func (needRestarter) Process() error { return nil }

// `mewla agent-use need` reaches the running daemon, which writes one card in
// Brain's thread: asked, then already asked, and no card for a grant that counts.
func TestAgentUseNeedAsksOnceThroughTheDaemon(t *testing.T) {
	root := t.TempDir()
	store := newControlBrainStore(t)
	service := brain.NewService(store, nil, nil)
	backend := &needBackend{state: map[string]grants.State{grants.Chrome: grants.Granted}}
	manager := grants.New(backend, grantCards{Store: store, Service: service}, needRestarter{})
	previous := grantsManager.Swap(manager)
	t.Cleanup(func() { grantsManager.Store(previous) })

	ctx, cancel := context.WithCancel(context.Background())
	cs := &control.Server{Path: filepath.Join(root, "run", control.SocketName), Handler: &controlApp{brainStore: store, brainService: service}}
	done := make(chan error, 1)
	go func() { done <- cs.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(cs.Path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	need := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := runAgentUseNeed(append(args, "--state-dir", root), &out, &bytes.Buffer{})
		return strings.TrimSpace(out.String()), err
	}
	if out, err := need("screen_recording", "--reason", "A Worker needs to see this Mac's screen."); err != nil || out != "asked" {
		t.Fatalf("first: %q %v", out, err)
	}
	if out, err := need("--reason", "Another.", "screen_recording"); err != nil || out != "already asked" {
		t.Fatalf("second: %q %v", out, err)
	}
	if out, err := need("chrome", "--reason", "Browser.", "--json"); err != nil {
		t.Fatal(err)
	} else {
		var got map[string]string
		if json.Unmarshal([]byte(out), &got) != nil || got["permission"] != "chrome" || got["outcome"] != "already granted" {
			t.Fatalf("json: %q", out)
		}
	}
	cards, _ := store.GrantRequests()
	if len(cards) != 1 || cards[0].GrantPermission != "screen_recording" || cards[0].Status != brain.GrantAsked {
		t.Fatalf("cards %+v", cards)
	}
	for _, args := range [][]string{{"location", "--reason", "x"}, {"chrome"}, {}, {"chrome", "extra", "--reason", "x"}} {
		if _, err := need(args...); err == nil {
			t.Fatalf("%v was accepted", args)
		}
	}
	// A daemon that is not running says so.
	if err := runAgentUseNeed([]string{"chrome", "--reason", "x", "--state-dir", t.TempDir()}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("no daemon, no error")
	}
}

// A Worker launch without the screen only for want of a grant names that grant;
// nothing else (off, unsupported, locked, a Chrome that is not installed) is one.
func TestMissingGrantNamesOnlyWhatAGrantFixes(t *testing.T) {
	permission := func(id string) agentuse.Status {
		return agentuse.Status{Enabled: true, Computer: &agentuse.ComputerStatus{State: agentuse.ComputerPermission, Permission: id}}
	}
	if p, reason, ok := missingGrant(permission("screen_recording"), "this Mac"); !ok || p != "screen_recording" || reason != "A Worker needs to see this Mac's screen." {
		t.Fatalf("%q %q %v", p, reason, ok)
	}
	if p, _, ok := missingGrant(permission("accessibility"), "this Mac"); !ok || p != "accessibility" {
		t.Fatalf("%q %v", p, ok)
	}
	if p, _, ok := missingGrant(agentuse.Status{Enabled: true, Browser: agentuse.BrowserStatus{State: agentuse.BrowserNoExtension}}, "this Mac"); !ok || p != "chrome" {
		t.Fatalf("%q %v", p, ok)
	}
	for _, status := range []agentuse.Status{
		{Enabled: true},
		{Enabled: false, Browser: agentuse.BrowserStatus{State: agentuse.BrowserNoExtension}},
		{Enabled: true, Browser: agentuse.BrowserStatus{State: agentuse.BrowserNoChrome}},
		{Enabled: true, Computer: &agentuse.ComputerStatus{State: agentuse.ComputerLocked}},
		{Enabled: true, Computer: &agentuse.ComputerStatus{State: agentuse.ComputerUnsupported}},
		{Enabled: true, Computer: &agentuse.ComputerStatus{State: agentuse.ComputerPermission}},
	} {
		if p, _, ok := missingGrant(status, "this Mac"); ok {
			t.Fatalf("%+v named %q", status, p)
		}
	}
}

// The card's grants are read from the screen service: missing, given but not
// counting yet (stale), or counting; its settings open the pane; Chrome is its
// browser status.
func TestGrantBackendReadsTheScreenAndChrome(t *testing.T) {
	fake := &screen.FakeBackend{Width: 10, Height: 10, Missing: []string{"screen_recording"}, Stale: []string{"accessibility"}}
	service := screen.NewServiceWith(t.TempDir(), fake)
	agent, err := agentuse.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	backend := grantBackend{screen: service, agentUse: agent}
	ctx := context.Background()
	if backend.State(ctx, grants.ScreenRecording) != grants.Missing || backend.State(ctx, grants.Accessibility) != grants.Stale {
		t.Fatal("missing and stale are not told apart")
	}
	fake.Missing, fake.Stale = nil, nil
	if backend.State(ctx, grants.ScreenRecording) != grants.Granted || backend.State(ctx, grants.Accessibility) != grants.Granted {
		t.Fatal("granted is not granted")
	}
	if err := backend.OpenSettings(ctx, grants.Accessibility); err != nil || len(fake.Opened) != 1 || fake.Opened[0] != "accessibility" {
		t.Fatalf("opened %v, %v", fake.Opened, err)
	}
	if backend.State(ctx, grants.Chrome) != grants.Missing {
		t.Fatal("Chrome is not set up here, yet counts")
	}
}

// Restarting for a granted Accessibility: the process restart is the Screen
// page's own (RestartNow, which launchd or systemd answer), in place where
// nothing supervises the daemon.
func TestGrantRestarterUsesTheScreenRestartUnderAServiceManager(t *testing.T) {
	service := screen.NewServiceWith(t.TempDir(), &screen.FakeBackend{Width: 10, Height: 10})
	var stopped, inPlace int
	r := grantRestarter{quiet: func() bool { return true }, inPlace: func() error { inPlace++; return nil }, screen: service, supervis: func() bool { return true }}
	if err := r.Process(); !errors.Is(err, screen.ErrNoRestart) {
		t.Fatalf("a service manager with no restart hook: %v", err)
	}
	service.SetRestart(func() { stopped++ })
	if err := r.Process(); err != nil || stopped != 1 || inPlace != 0 {
		t.Fatalf("stopped %d, in place %d, %v", stopped, inPlace, err)
	}
	r.supervis = func() bool { return false }
	if err := r.Process(); err != nil || stopped != 1 || inPlace != 1 {
		t.Fatalf("stopped %d, in place %d, %v", stopped, inPlace, err)
	}
}

// The daemon's own in-place restart (daemon_upgrade) is what the card asks for first.
func TestInPlaceRestartAsksTheDaemonToRestartItself(t *testing.T) {
	var got *daemonReexec
	restarter := &inPlaceRestarter{
		startup: startupSnapshot{Executable: "/bin/mewla"},
		probe:   func(string) (string, error) { return "0.4.17", nil },
		restart: func(r *daemonReexec) { got = r },
	}
	if err := inPlaceRestart(restarter); err != nil || got == nil || got.version != "0.4.17" {
		t.Fatalf("%v %+v", err, got)
	}
	if err := inPlaceRestart(restarter); err == nil || !strings.Contains(err.Error(), "already restarting") {
		t.Fatalf("a second restart: %v", err)
	}
	if err := inPlaceRestart(nil); err == nil {
		t.Fatal("no restarter, no error")
	}
}
