package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/control"
)

// The command's own look never fetches the browser tools, so it must not be
// what Workers read: "Getting ready" stayed up on a Mac where nothing was
// fetching them.
func TestAgentUseStatusIsTheDaemons(t *testing.T) {
	// Short: a Unix socket path has a length limit.
	root, err := os.MkdirTemp("", "m")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	waiting := agentuse.Status{Enabled: true, Browser: agentuse.BrowserStatus{State: agentuse.BrowserPreparing, Summary: "Getting the browser tools ready…"}}

	got := daemonAgentUse(root, waiting)
	if got.Browser.State != agentuse.BrowserPreparing || !strings.Contains(got.Browser.Summary, "Mewla isn't running here") {
		t.Fatalf("no daemon: %+v", got)
	}

	daemon, err := agentuse.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	daemon.SetComputer(agentuse.ComputerHooks{Status: func() agentuse.ComputerStatus {
		return agentuse.ComputerStatus{State: agentuse.ComputerReady, Summary: "the daemon's screen"}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	cs := &control.Server{Path: filepath.Join(root, "run", control.SocketName), Handler: &controlApp{agentUse: daemon}}
	done := make(chan error, 1)
	go func() { done <- cs.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(cs.Path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got = daemonAgentUse(root, waiting)
	if got.Computer == nil || got.Computer.Summary != "the daemon's screen" || got.Browser.State == agentuse.BrowserPreparing {
		t.Fatalf("daemon: %+v", got)
	}
}
