package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeHost struct{ alive atomic.Bool }

func (h *fakeHost) Alive() bool  { return h.alive.Load() }
func (h *fakeHost) Close() error { h.alive.Store(false); return nil }

type fakeBackend struct {
	mu      sync.Mutex
	starts  int
	started chan struct{}
	err     error
	hosts   []*fakeHost
}

func (b *fakeBackend) Capability(context.Context) Capability { return Capability{Available: true} }
func (b *fakeBackend) Start(context.Context, string) (Host, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	b.starts++
	h := &fakeHost{}
	h.alive.Store(true)
	b.hosts = append(b.hosts, h)
	return h, nil
}

type fakeScreens struct {
	mu      sync.Mutex
	windows map[string]int
}

func (s *fakeScreens) DisplayEnv(context.Context) (int, string, error) { return 100, "/x", nil }
func (s *fakeScreens) Monitor(context.Context, string) (image.Rectangle, error) {
	return image.Rect(1280, 0, 2560, 800), nil
}
func (s *fakeScreens) Windows(worker string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.windows == nil {
		s.windows = map[string]int{}
	}
	s.windows[worker] = n
}
func (s *fakeScreens) count(worker string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windows[worker]
}

func newManager(t *testing.T, b *fakeBackend, s *fakeScreens) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), b, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestConcurrentFirstWindowsShareOneBrowser(t *testing.T) {
	b, s := &fakeBackend{}, &fakeScreens{}
	m := newManager(t, b, s)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.EnsureHost(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if b.starts != 1 {
		t.Fatalf("the browser started %d times", b.starts)
	}
	for _, w := range []string{"a", "b"} {
		if err := m.Opened(w, "key-"+w); err != nil {
			t.Fatal(err)
		}
	}
	if s.count("a") != 1 || s.count("b") != 1 {
		t.Fatalf("windows %v", s.windows)
	}
}

func TestStartFailureNamesItsCauseAndLeavesNothingStuck(t *testing.T) {
	b := &fakeBackend{err: errors.New("Xvnc could not start")}
	m := newManager(t, b, &fakeScreens{})
	if err := m.EnsureHost(context.Background()); err == nil || err.Error() != "Xvnc could not start" {
		t.Fatalf("err = %v", err)
	}
	b.mu.Lock()
	b.err = nil
	b.mu.Unlock()
	if err := m.EnsureHost(context.Background()); err != nil {
		t.Fatalf("the next try: %v", err)
	}
}

func TestAWindowAgainReplacesTheOldAndClosedTakesItOff(t *testing.T) {
	s := &fakeScreens{}
	m := newManager(t, &fakeBackend{}, s)
	_ = m.Opened("w", "k1")
	_ = m.Opened("w", "k2")
	if s.count("w") != 1 {
		t.Fatalf("a Worker has %d windows", s.count("w"))
	}
	m.Closed("k1") // the old one: nothing changes for the new
	if s.count("w") != 1 {
		t.Fatalf("closing the replaced window changed the count to %d", s.count("w"))
	}
	m.Closed("k2")
	if s.count("w") != 0 {
		t.Fatalf("the window stayed: %d", s.count("w"))
	}
}

func TestForgetClosesTheWindowAndTheBrowserStopsWithTheLast(t *testing.T) {
	old := hostGrace
	hostGrace = 0
	t.Cleanup(func() { hostGrace = old })
	b, s := &fakeBackend{}, &fakeScreens{}
	m := newManager(t, b, s)
	var closed []string
	var mu sync.Mutex
	m.SetWindowCloser(func(key string) { mu.Lock(); closed = append(closed, key); mu.Unlock() })
	if err := m.EnsureHost(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = m.Opened("a", "ka")
	_ = m.Opened("b", "kb")
	m.Forget("a")
	mu.Lock()
	if len(closed) != 1 || closed[0] != "ka" {
		t.Fatalf("closed %v", closed)
	}
	mu.Unlock()
	if s.count("a") != 0 || s.count("b") != 1 {
		t.Fatalf("windows %v", s.windows)
	}
	time.Sleep(100 * time.Millisecond)
	if !b.hosts[0].Alive() {
		t.Fatal("the browser stopped with a window still open")
	}
	m.Forget("b")
	deadline := time.Now().Add(2 * time.Second)
	for b.hosts[0].Alive() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if b.hosts[0].Alive() {
		t.Fatal("the browser did not stop with its last window")
	}
}

func TestSweepClosesWindowsOfWorkersThatEnded(t *testing.T) {
	old := sweepEvery
	sweepEvery = 20 * time.Millisecond
	t.Cleanup(func() { sweepEvery = old })
	s := &fakeScreens{}
	m := newManager(t, &fakeBackend{}, s)
	var live atomic.Bool
	live.Store(true)
	m.SetWorkerLive(func(string) bool { return live.Load() })
	_ = m.Opened("w", "k")
	time.Sleep(100 * time.Millisecond)
	if s.count("w") != 1 {
		t.Fatal("a live Worker lost its window")
	}
	live.Store(false)
	deadline := time.Now().Add(2 * time.Second)
	for s.count("w") != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.count("w") != 0 {
		t.Fatal("the window of an ended Worker stayed")
	}
}

func TestADeadBrowserTakesItsWindowsOffAndStartsAgain(t *testing.T) {
	b, s := &fakeBackend{}, &fakeScreens{}
	m := newManager(t, b, s)
	_ = m.EnsureHost(context.Background())
	_ = m.Opened("w", "k")
	b.hosts[0].alive.Store(false) // Chrome crashed
	if err := m.EnsureHost(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.starts != 2 || s.count("w") != 0 {
		t.Fatalf("starts %d, windows %v", b.starts, s.windows)
	}
}

func TestOnlyTheSettingsAreAsked(t *testing.T) {
	m := newManager(t, &fakeBackend{}, &fakeScreens{})
	on := true
	r, err := m.Handle(context.Background(), Request{Action: "settings", Enabled: &on})
	if err != nil || r.VirtualScreen == nil || !*r.VirtualScreen || !m.VirtualScreen() {
		t.Fatalf("settings: %v %+v", err, r)
	}
	if raw, _ := os.ReadFile(filepath.Join(m.root, "settings.json")); !json.Valid(raw) {
		t.Fatal("the switch was not kept")
	}
	for _, a := range []string{"list", "control", "halt", "begin"} {
		if _, err := m.Handle(context.Background(), Request{Action: a}); err == nil {
			t.Fatalf("%s was accepted", a)
		}
	}
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
		m, err := New(root, &fakeBackend{}, nil)
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

// Only Linux has a virtual screen; elsewhere the answer says so, and a Linux
// answer stays as it was.
func TestSettingsSayWhenThisSystemHasNoVirtualScreen(t *testing.T) {
	m := newManager(t, &fakeBackend{}, &fakeScreens{})
	for _, system := range []bool{true, false} {
		previous := virtualScreenSystem
		virtualScreenSystem = system
		r, err := m.Handle(context.Background(), Request{Action: "settings"})
		virtualScreenSystem = previous
		if err != nil || r.VirtualScreenUnsupported == system {
			t.Fatalf("system has a virtual screen=%v: %v %+v", system, err, r)
		}
		raw, _ := json.Marshal(r)
		if system && strings.Contains(string(raw), "virtual_screen_unsupported") {
			t.Fatalf("a Linux answer gained a field: %s", raw)
		}
	}
}
