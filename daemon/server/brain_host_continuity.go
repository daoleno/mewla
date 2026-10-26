package server

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// brainHostContinuityRetryDelays bounds re-runs after a transient ownership
// probe failure or a Host still launching (its first children, or a login
// shell that has not yet started the provider).
var brainHostContinuityRetryDelays = []time.Duration{
	2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute,
}

// brainHostContinuityRetryDelay returns the wait before the next attempt, or
// false when err is not transient or the bounded schedule is exhausted. A Host
// whose launch shell has not yet reached the provider is transient too: the
// activation was held back, not lost.
func brainHostContinuityRetryDelay(attempt int, err error) (time.Duration, bool) {
	transient := errors.Is(err, watcher.ErrOwnershipProbeUnavailable) || errors.Is(err, watcher.ErrWorkerInputNotReady)
	if !transient || attempt >= len(brainHostContinuityRetryDelays) {
		return 0, false
	}
	return brainHostContinuityRetryDelays[attempt], true
}

// Restore once after initial discovery, then only when the watcher reports
// removal of the current Host. A transient ownership-probe failure is retried
// on a short bounded schedule; there is no periodic probe. Keeping recovery
// outside broadcastEvents lets discovery/output events drain while a
// replacement waits for provider readiness.
func (s *Server) runBrainHostContinuity(ctx context.Context) {
	if s.brain == nil || s.watcher == nil {
		return
	}
	if err := s.watcher.WaitForSnapshot(ctx); err != nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	attempt := 0
	var retry <-chan time.Time
	reconcile := func() {
		err := s.reconcileBrainHostContinuity()
		delay, ok := brainHostContinuityRetryDelay(attempt, err)
		if !ok {
			attempt, retry = 0, nil
			return
		}
		attempt++
		log.Printf("brain Host continuity retry %d/%d in %s", attempt, len(brainHostContinuityRetryDelays), delay)
		retry = time.After(delay)
	}
	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.brainHostRecovery:
			if ctx.Err() != nil {
				return
			}
			attempt = 0
			reconcile()
		case <-retry:
			if ctx.Err() != nil {
				return
			}
			reconcile()
		}
	}
}

func (s *Server) reconcileBrainHostContinuity() error {
	if s.brain == nil {
		return nil
	}
	changed, err := s.brain.ReconcileHostContinuity()
	if changed {
		s.broadcastBrainHostCapabilityRefresh()
	}
	if err != nil {
		log.Printf("brain Host continuity reconciliation failed: %v", err)
	}
	return err
}
