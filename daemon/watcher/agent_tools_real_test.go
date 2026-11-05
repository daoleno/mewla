package watcher

import (
	"github.com/daoleno/mewla/daemon/agentevents"
	"sync"
	"testing"
)

// Visible launches gain this computer's agent tools; hidden ones (the Brain
// host) keep their command.
func TestRealAgentToolsApplyOnlyToVisibleLaunches(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	var mu sync.Mutex
	var seen []string
	var workers []bool
	h.w.SetAgentTools(func(command string, worker bool) string {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, command)
		workers = append(workers, worker)
		return command
	})
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "worker", Command: "exec /bin/sh", Detached: true, ProgressEnv: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "shell", Command: "exec /bin/sh", Detached: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "Brain", Command: "exec /bin/sh -i", Detached: true, Hidden: true}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "exec /bin/sh" || !workers[0] || workers[1] {
		t.Fatalf("agent tools saw %q (worker identity %v)", seen, workers)
	}
}

// The tools go on the command before the launch adapter wraps it: Codex's
// adapter turns the command into shell with an app-server, which tool
// injection cannot parse, so a Codex Worker would have had no tools.
func TestRealAgentToolsAreAddedBeforeTheLaunchAdapterWrapsTheCommand(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	h.w.SetAgentTools(func(command string, worker bool) string { return command + " # tools" })
	var adapted string
	h.w.SetLaunchAdapter(func(executorID, command string) (agentevents.Launch, error) {
		adapted = command
		return agentevents.Launch{Command: command}, nil
	})
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "worker", Command: "exec /bin/sh", Detached: true, ProgressEnv: true}); err != nil {
		t.Fatal(err)
	}
	if adapted != "exec /bin/sh # tools" {
		t.Fatalf("the adapter saw %q, without the tools", adapted)
	}
}
