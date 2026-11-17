package main

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/selfupdate"
	"github.com/daoleno/mewla/daemon/watcher"
)

// minHostVersion is the oldest session host this daemon keeps. The host
// outlives every daemon update (that is how Sessions survive one), so a fix
// in the host reaches a computer only when its host restarts, and a restart
// ends every terminal in it. Raise this only when a host-side fix must reach
// running hosts: 0.4.7 has the last ones (agents found on the login shell's
// PATH, and on Windows in ~/.local/bin installed after Mewla started).
const minHostVersion = "0.4.7"

const hostUpgradePoll = time.Minute

// hostUpgrader restarts a session host older than minHostVersion at the
// first moment nothing would be lost: no Session alive but Brain's, Brain
// between turns, and nothing the daemon carries in flight. Brain comes back in
// its conversation through host recovery. Until that moment the app offers a
// one-tap restart.
type hostUpgrader struct {
	supported bool
	state     func() (watcher.SessionHostState, error)
	restart   func(allowed func(watcher.SessionHostState) bool) (bool, error)
	// brainHost is Brain's host Session id, "" when there is none.
	brainHost func() string
	workers   func() []*classifier.Worker
	// quiet names what the daemon carries in flight (updateQuietPoint).
	quiet func() string
	poll  time.Duration

	mu          sync.Mutex
	hostVersion string
	behind      bool
	alive       int
	restarting  bool
}

// hostStatus is what the app is told about the session host.
type hostStatus struct {
	Version string
	// Restart: the host is behind and waits for a restart.
	Restart bool
	// Terminals is how many terminals a restart now would end.
	Terminals int
}

// hostUpgradeSupported: only release builds restart a host on their own; a
// development daemon never touches the host its developer works in.
func hostUpgradeSupported() bool {
	return releaseBuild == "1" && os.Getenv(selfupdate.ChildEnv) == "" && os.Getenv("MEWLA_NO_AUTOUPDATE") == ""
}

// hostBehind: a host older than minHostVersion. A version that does not
// parse (a development host) is never behind.
func hostBehind(version string) bool {
	return selfupdate.Older(version, minHostVersion)
}

// Status is the running host's version and whether it waits for a restart.
func (h *hostUpgrader) Status() hostStatus {
	if h == nil {
		return hostStatus{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return hostStatus{Version: h.hostVersion, Restart: h.behind && h.supported, Terminals: h.alive}
}

// Run looks every poll and restarts a host that is behind once nothing would
// be lost.
func (h *hostUpgrader) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(h.poll):
		}
		h.tick()
	}
}

func (h *hostUpgrader) tick() {
	state, err := h.state()
	if err != nil {
		return // no host runs: the next one is this version
	}
	behind := hostBehind(state.Version)
	h.mu.Lock()
	h.hostVersion, h.behind, h.alive = state.Version, behind, len(state.Alive)
	h.mu.Unlock()
	if !behind || !h.supported {
		return
	}
	if reason := h.lossOf(state); reason != "" {
		return
	}
	h.restartHost(false)
}

// lossOf names what restarting the host now would end, "" when nothing.
func (h *hostUpgrader) lossOf(state watcher.SessionHostState) string {
	brain := h.brainHost()
	for _, id := range state.Alive {
		if id != brain {
			return "a running terminal"
		}
		if !h.brainBetweenTurns(id) {
			return "Brain's turn"
		}
	}
	if reason := h.quiet(); reason != "" {
		return reason
	}
	return ""
}

// brainBetweenTurns: Brain's Session is neither working nor waiting on an
// answer.
func (h *hostUpgrader) brainBetweenTurns(id string) bool {
	for _, worker := range h.workers() {
		if worker.ID == id {
			return worker.State != classifier.StateRunning && worker.State != classifier.StateBlocked
		}
	}
	return false // not observed yet: not known to be idle
}

// RestartNow is the app's one tap: the person ends every terminal now.
func (h *hostUpgrader) RestartNow() error {
	if h == nil || !h.supported {
		return errors.New("this Mewla does not restart its terminals")
	}
	go h.restartHost(true)
	return nil
}

func (h *hostUpgrader) restartHost(now bool) {
	h.mu.Lock()
	if h.restarting {
		h.mu.Unlock()
		return
	}
	h.restarting = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.restarting = false
		h.mu.Unlock()
	}()
	restarted, err := h.restart(func(state watcher.SessionHostState) bool {
		// Checked again at the last moment: a Worker may have started.
		return now || h.lossOf(state) == ""
	})
	if err != nil {
		log.Printf("session host: restart onto this version: %v", err)
	}
	if restarted {
		h.tick()
	}
}
