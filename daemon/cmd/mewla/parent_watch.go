package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"
)

// exitWithParentEnv is set by an app that runs its own daemon for as long as
// it runs (Mewla Dev for Mac, client/macos/src/daemon.rs): the parent's pid.
// The daemon stops once that process is gone, however it ended (a quit, a
// SIGTERM or SIGKILL that skipped the app's own stop).
const exitWithParentEnv = "MEWLA_EXIT_WITH_PARENT"

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
