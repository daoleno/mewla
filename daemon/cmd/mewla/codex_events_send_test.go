package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/watcher"
)

func TestCodexSendLeavesAWorkerWithoutAConnectedThreadToThePane(t *testing.T) {
	events := newCodexEvents(context.Background(), agentevents.NewHub())
	handled, err := events.send("%1", "hello", "r-1", func() error { t.Fatal("begin ran for a Worker with nothing to send to"); return nil })
	if handled || err != nil {
		t.Fatalf("send to an unknown Worker = (%v, %v), want it left to the pane", handled, err)
	}
	events.sessions["%1"] = &codexSession{workerID: "%1"}
	handled, err = events.send("%1", "hello", "r-1", func() error { t.Fatal("begin ran for a Worker with nothing to send to"); return nil })
	if handled || err != nil {
		t.Fatalf("send to a Worker not yet connected = (%v, %v), want it left to the pane", handled, err)
	}
}

func TestCodexInterruptNeverTypesCtrlCAtAnIdleCodex(t *testing.T) {
	events := newCodexEvents(context.Background(), agentevents.NewHub())
	if handled, err := events.interrupt("%1"); handled || err != nil {
		t.Fatalf("interrupt of an unknown Worker = (%v, %v), want it left to the pane", handled, err)
	}
	events.sessions["%1"] = &codexSession{workerID: "%1", client: &codexctl.Client{}, thread: "t-1"}
	if handled, err := events.interrupt("%1"); !handled || err != nil {
		t.Fatalf("interrupt with no running turn = (%v, %v), want handled with nothing to stop", handled, err)
	}
}

func TestCodexObserveFollowsAProfileRoutedWorkerOnceTheRouteTableKnowsItsSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := newCodexEvents(ctx, agentevents.NewHub())
	table := map[string]string{}
	var mu sync.Mutex
	events.SetSocketSource(func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		return table[id]
	})
	worker := &classifier.Worker{ID: "%7", Cwd: "/work"}
	events.Observe(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "%7", Worker: worker})
	if _, ok := events.sessions["%7"]; ok {
		t.Fatal("a Worker with no socket anywhere got a session")
	}
	mu.Lock()
	table["%7"] = filepath.Join(t.TempDir(), "none.sock")
	mu.Unlock()
	events.Observe(watcher.SessionEvent{Type: "worker_state_change", WorkerID: "%7", Worker: worker})
	events.mu.Lock()
	session := events.sessions["%7"]
	events.mu.Unlock()
	if session == nil || session.cwd != "/work" {
		t.Fatalf("session after the route table learned the socket = %+v", session)
	}
	events.Observe(watcher.SessionEvent{Type: "worker_removed", WorkerID: "%7"})
}
