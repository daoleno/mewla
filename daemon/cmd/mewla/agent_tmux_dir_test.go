package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTmuxDirIsNamespacedByTheDaemon(t *testing.T) {
	home := "/home/user"
	daemonID := strings.Repeat("ab", 32)
	got, err := agentTmuxDir(home, daemonID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".mewla", "run", "tmux-scratch", daemonID[:24]); got != want {
		t.Fatalf("dir = %q, want %q", got, want)
	}
	for _, id := range []string{"", "   "} {
		if dir, err := agentTmuxDir(home, id); err == nil {
			t.Fatalf("empty identity %q produced %q", id, dir)
		}
	}
}

// A Session is named by the MEWLA_SESSION_ID the session host sets, never by
// a TMUX_PANE from the person's own tmux.
func TestCurrentWorkerIDComesFromTheSession(t *testing.T) {
	t.Setenv("MEWLA_WORKER_ID", "")
	t.Setenv("MEWLA_SESSION_ID", "")
	t.Setenv("TMUX_PANE", "%7")
	if got := currentWorkerID(); got != "" {
		t.Fatalf("currentWorkerID() = %q outside a Session", got)
	}
	t.Setenv("MEWLA_SESSION_ID", "mh-0123456789abcdef01234567")
	if got := currentWorkerID(); got != "mh-0123456789abcdef01234567" {
		t.Fatalf("currentWorkerID() = %q", got)
	}
	t.Setenv("MEWLA_WORKER_ID", "mh-aaaaaaaaaaaaaaaaaaaaaaaa")
	if got := currentWorkerID(); got != "mh-aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("currentWorkerID() = %q, want MEWLA_WORKER_ID", got)
	}
}
