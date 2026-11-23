package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

// exitWithParentEnv is set by an app that runs its own daemon for as long as
// it runs (Mewla Dev for Mac, client/macos/src/daemon.rs): the parent's pid.
// The daemon stops once that process is gone, however it ended (a quit, a
// SIGTERM or SIGKILL that skipped the app's own stop).
const exitWithParentEnv = "MEWLA_EXIT_WITH_PARENT"

// parentGone is set once the daemon stops because its parent is gone: the
// session host, which outlives a daemon restart by design, then ends too.
var parentGone atomic.Bool

// stopWithParent stops the daemon (stop) once its parent is no longer the
// process named by MEWLA_EXIT_WITH_PARENT; nothing without it.
func stopWithParent(ctx context.Context, stop context.CancelCauseFunc) {
	value := os.Getenv(exitWithParentEnv)
	// The daemon's alone: what it starts (Brain, Workers, a nested test daemon) does not inherit it.
	_ = os.Unsetenv(exitWithParentEnv)
	parent, err := strconv.Atoi(value)
	if err != nil || parent <= 1 {
		return
	}
	go watchParent(ctx, parent, os.Getppid, time.Second, func() {
		log.Printf("mewla: stopping: the app that started it (pid %d) is gone", parent)
		parentGone.Store(true)
		stop(nil)
	})
}

// stopWithHostEnv is set by Mewla for Mac's own executable when launchd runs
// the daemon through it (client/macos/src/host.rs), so that macOS charges the
// daemon's permissions to the app: the host's pid. Should the host end without
// passing on launchd's stop (a SIGKILL), the daemon stops as on any stop; the
// session host and its Sessions run on, as on a daemon restart.
const stopWithHostEnv = "MEWLA_STOP_WITH_HOST"

// stopWithHost stops the daemon once its parent is no longer the process
// named by MEWLA_STOP_WITH_HOST; nothing without it. Unlike stopWithParent,
// the session host is left running.
func stopWithHost(ctx context.Context, stop context.CancelCauseFunc) {
	value := os.Getenv(stopWithHostEnv)
	_ = os.Unsetenv(stopWithHostEnv)
	host, err := strconv.Atoi(value)
	if err != nil || host <= 1 {
		return
	}
	go watchParent(ctx, host, os.Getppid, time.Second, func() {
		log.Printf("mewla: stopping: the app's host (pid %d) that runs it is gone", host)
		stop(nil)
	})
}

// watchParent calls gone once ppid() is no longer parent (an orphan is
// adopted by launchd or init), checking every interval until ctx ends.
func watchParent(ctx context.Context, parent int, ppid func() int, interval time.Duration, gone func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ppid() != parent {
			gone()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// stopSessionHostWithParent ends this state's session host and the Sessions
// in it once the daemon has stopped for its parent: nothing of the app's
// daemon is left running after the app. stateDir is the resolved one the
// host was configured with (configureSessionHost), not the -state-dir flag.
func stopSessionHostWithParent(stateDir string) {
	if !parentGone.Load() {
		return
	}
	path, err := sessionhost.SocketPath(stateDir)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := sessionhost.Dial(ctx, path, hostVersion())
	if err != nil {
		log.Printf("mewla: no session host to stop at %s: %v", path, err)
		return
	}
	defer client.Close()
	if err := client.Shutdown(ctx, true); err != nil {
		log.Printf("mewla: the session host did not stop: %v", err)
		return
	}
	log.Printf("mewla: the session host stopped with the app")
}
