package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// spawnExitWindow is how long after its launch a Worker's agent is watched
// for exiting before spawn reports success. An agent that cannot run (not on
// the Session's PATH, a runtime missing, an option it refuses) exits within
// it; before, spawn answered ok and the Worker sat "awaiting provider
// admission" with nothing to show why.
const spawnExitWindow = 2500 * time.Millisecond

type sessionExitReporter interface {
	SessionExitReport(id string) (watcher.SessionExit, bool)
}

// spawnedAgentExit is the exit of a just-spawned Worker's agent, waiting up to
// spawnExitWindow from launchedAt; false while it runs.
func (a *controlApp) spawnedAgentExit(workerID string, launchedAt time.Time) (watcher.SessionExit, bool) {
	reporter, ok := a.watcher.(sessionExitReporter)
	if !ok {
		return watcher.SessionExit{}, false
	}
	for {
		if exit, exited := reporter.SessionExitReport(workerID); exited {
			return exit, true
		}
		if time.Since(launchedAt) >= spawnExitWindow {
			return watcher.SessionExit{}, false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// spawnExitError is a Worker launch whose agent exited, in plain words with
// its output, logged with the command it ran.
func spawnExitError(workerID, command string, exit watcher.SessionExit) error {
	program := command
	if fields := strings.Fields(command); len(fields) > 0 {
		program = fields[0]
	}
	log.Printf("worker launch failed: session=%s command=%q ran=%s exit=%s last output: %q",
		workerID, command, exit.Ran.Round(time.Millisecond), exit.Status(), exit.TailLine(12, 1200))
	return &agentExitedError{message: fmt.Sprintf("the Worker's agent did not start: %s (Session %s; its terminal shows the output)",
		strings.TrimSuffix(exit.Describe(program), "."), workerID)}
}

// agentExitedError is a Worker launch whose agent exited.
type agentExitedError struct{ message string }

func (e *agentExitedError) Error() string { return e.message }
