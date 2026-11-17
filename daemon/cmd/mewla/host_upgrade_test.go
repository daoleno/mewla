package main

import (
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

type fakeSessionHost struct {
	mu       sync.Mutex
	state    watcher.SessionHostState
	restarts int
}

func (f *fakeSessionHost) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarts
}

func (f *fakeSessionHost) read() (watcher.SessionHostState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeSessionHost) restart(allowed func(watcher.SessionHostState) bool) (bool, error) {
	if !allowed(f.state) {
		return false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	f.state = watcher.SessionHostState{Version: "0.4.15"}
	return true, nil
}

func newHostUpgraderFixture(version string, alive ...string) (*hostUpgrader, *fakeSessionHost, *[]*classifier.Worker, *string) {
	host := &fakeSessionHost{state: watcher.SessionHostState{Version: version, Alive: alive}}
	workers := []*classifier.Worker{{ID: "mh-brain", State: classifier.StateDone}}
	quiet := ""
	h := &hostUpgrader{
		supported: true, state: host.read, restart: host.restart,
		brainHost: func() string { return "mh-brain" },
		workers:   func() []*classifier.Worker { return workers },
		quiet:     func() string { return quiet },
		poll:      time.Millisecond,
	}
	return h, host, &workers, &quiet
}

func TestAnOldHostRestartsOnlyWhenNothingIsLost(t *testing.T) {
	h, host, workers, quiet := newHostUpgraderFixture("0.4.6", "mh-brain", "mh-worker")
	h.tick()
	if host.restarts != 0 {
		t.Fatal("restarted the host with a Worker's terminal alive")
	}
	if status := h.Status(); status.Version != "0.4.6" || !status.Restart || status.Terminals != 2 {
		t.Fatalf("status = %+v, want the old host reported as waiting for a restart that ends 2 terminals", status)
	}
	host.state.Alive = []string{"mh-brain"}
	(*workers)[0].State = classifier.StateRunning
	h.tick()
	if host.restarts != 0 {
		t.Fatal("restarted the host in the middle of Brain's turn")
	}
	(*workers)[0].State = classifier.StateDone
	*quiet = "a permission answer"
	h.tick()
	if host.restarts != 0 {
		t.Fatal("restarted the host while a permission decision was held open")
	}
	*quiet = ""
	h.tick()
	if host.restarts != 1 {
		t.Fatal("an old host with only an idle Brain was not restarted")
	}
	if status := h.Status(); status.Version != "0.4.15" || status.Restart {
		t.Fatalf("after the restart status = %+v", status)
	}
}

func TestACurrentOrDevelopmentHostIsLeftAlone(t *testing.T) {
	for _, version := range []string{"0.4.7", "0.4.8", "dev", ""} {
		h, host, _, _ := newHostUpgraderFixture(version)
		h.tick()
		if status := h.Status(); host.restarts != 0 || status.Restart {
			t.Fatalf("host %q: restarts %d, status %+v", version, host.restarts, status)
		}
	}
	h, host, _, _ := newHostUpgraderFixture("0.4.6")
	h.supported = false // a development daemon never restarts its developer's host
	h.tick()
	if status := h.Status(); host.restarts != 0 || status.Restart {
		t.Fatalf("development daemon: restarts %d, status %+v", host.restarts, status)
	}
}

func TestRestartTerminalsNowEndsEveryTerminal(t *testing.T) {
	h, host, _, _ := newHostUpgraderFixture("0.4.6", "mh-brain", "mh-worker", "mh-shell")
	h.tick()
	if err := h.RestartNow(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if host.count() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the person's tap did not restart the host")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
