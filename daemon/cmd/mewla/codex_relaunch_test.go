package main

import (
	"context"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A relaunched Codex Worker comes back on a new app-server socket (its old
// one died with the TUI): what Mewla sends, approves and interrupts through
// follows it there, and nothing goes to the old one any more.
func TestCodexRelaunchedWorkerIsFollowedOnItsNewAppServer(t *testing.T) {
	old, oldSocket := startStopAppServer(t, nil, 0)
	fresh, freshSocket := startStopAppServer(t, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := newCodexEvents(ctx, agentevents.NewHub())
	connected := func(socket string) *codexSession {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if s := events.session("%1"); s != nil && s.socket == socket {
				if client, _ := s.connection(); client != nil {
					return s
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no connected session on %s", socket)
		return nil
	}
	worker := &classifier.Worker{ID: "%1", Cwd: "/work", AgentControl: oldSocket}
	events.Observe(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "%1", Worker: worker})
	first := connected(oldSocket)
	first.setThread("t-1")

	relaunched := &classifier.Worker{ID: "%1", Cwd: "/work", AgentControl: freshSocket}
	events.Observe(watcher.SessionEvent{Type: "worker_metadata_change", WorkerID: "%1", Worker: relaunched})
	second := connected(freshSocket)
	second.setThread("t-1")
	if client, _ := first.connection(); client != nil {
		t.Fatal("the old app-server's session still holds a connection")
	}
	// The double answers turn/start without a turn id; where it went is the
	// point.
	if handled, _ := events.send("%1", "continue", "r-1", func() error { return nil }); !handled {
		t.Fatal("send was left to the pane")
	}
	methods, _ := fresh.calls()
	if !contains(methods, "turn/start") {
		t.Fatalf("new app-server was asked %v", methods)
	}
	if methods, _ := old.calls(); contains(methods, "turn/start") {
		t.Fatalf("the old app-server was sent to: %v", methods)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
