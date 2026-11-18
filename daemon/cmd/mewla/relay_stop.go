package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// A daemon stop or restart must not end a Worker's model reply: every Worker
// reaches its model through this daemon's loopback relay. So when the daemon
// is asked to stop, the relay drains first (modelprofiles.Owner.Drain) while
// everything else keeps running; only then does the rest of the daemon shut
// down. On a restart the relay's listening sockets stay open across it
// (listenhandoff), so a request made in between waits for the new daemon
// instead of being refused, and the control socket gets a restart marker so
// commands wait too (control.MarkRestarting).
const (
	// restartDrainCap bounds the wait for replies on a restart, which keeps
	// every socket open: nothing is refused meanwhile, new replies just wait.
	restartDrainCap = 5 * time.Minute
	// stopDrainCap bounds it on a plain stop (a signal: a person, systemd,
	// launchd). It stays inside launchd's 20 s exit timeout.
	stopDrainCap = 15 * time.Second
	// restartStartAllowance is how long a restarting daemon may take, after
	// its drain, to listen again.
	restartStartAllowance = 2 * time.Minute
)

type relayStop struct {
	mu         sync.Mutex
	drain      func(context.Context) modelprofiles.DrainResult
	socketPath string
}

// set names the relay to drain and the control socket a restart marks.
func (r *relayStop) set(drain func(context.Context) modelprofiles.DrainResult, socketPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drain, r.socketPath = drain, socketPath
}

// run waits for a stop to be requested, drains the relay, then ends the
// daemon's runtime with the same cause.
func (r *relayStop) run(requested context.Context, endRuntime context.CancelCauseFunc) {
	<-requested.Done()
	cause := context.Cause(requested)
	r.mu.Lock()
	drain, socketPath := r.drain, r.socketPath
	r.mu.Unlock()
	if drain == nil {
		endRuntime(cause)
		return
	}
	restart := keepsProcessForRestart(cause)
	limit := stopDrainCap
	if restart {
		limit = restartDrainCap
	}
	if restart && socketPath != "" {
		if err := control.MarkRestarting(socketPath, os.Getpid(), time.Now().Add(limit+restartStartAllowance)); err != nil {
			log.Printf("mewla: mark the restart for waiting commands: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	// Asked again to stop: stop now.
	again := make(chan os.Signal, 1)
	signal.Notify(again, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case received := <-again:
			log.Printf("mewla: %v again; not waiting for model replies", received)
			cancel()
		case <-ctx.Done():
		}
	}()
	started := time.Now()
	result := drain(ctx)
	signal.Stop(again)
	cancel()
	if result.Waited > 0 || result.Cut > 0 {
		log.Printf("mewla: model relay drained in %s: %d replies waited for, %d cut; kept open for the next daemon: %s",
			time.Since(started).Round(time.Millisecond), result.Waited, result.Cut, strings.Join(result.Held, ", "))
	}
	endRuntime(cause)
}

// keepsProcessForRestart: the stop is a restart this process carries out
// itself (an in-place restart, an update handover), so its pid answers again.
func keepsProcessForRestart(cause error) bool {
	var reexec *daemonReexec
	var handover *updateHandover
	return errors.As(cause, &reexec) || errors.As(cause, &handover)
}
