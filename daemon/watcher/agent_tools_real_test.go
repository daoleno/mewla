package watcher

import (
	"sync"
	"testing"
)

// Visible launches gain this computer's agent tools; hidden ones (the Brain
// host) keep their command.
func TestRealAgentToolsApplyOnlyToVisibleLaunches(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	var mu sync.Mutex
	var seen []string
	h.w.SetAgentTools(func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, command)
		return command
	})
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "worker", Command: "exec /bin/sh", Detached: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.w.CreateSession("", CreateSessionOptions{Name: "Brain", Command: "exec /bin/sh -i", Detached: true, Hidden: true}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "exec /bin/sh" {
		t.Fatalf("agent tools saw %q", seen)
	}
}
