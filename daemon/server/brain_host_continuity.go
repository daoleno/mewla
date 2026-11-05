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

// brainHostRecoveryInterval is how often a Brain with no host checks whether its
// agent can run now; brainHostRecoveryMaxHold caps the wait after failed starts.
const (
	brainHostRecoveryInterval = 15 * time.Second
	brainHostRecoveryMaxHold  = 5 * time.Minute
)

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
	// A host that was not runnable (agent not installed or signed out) or died
	// starts again once its cause is fixed, without restarting Mewla. Probing is
	// a PATH lookup and a cached sign-in answer; failed starts back off.
	recovery := time.NewTicker(brainHostRecoveryInterval)
	defer recovery.Stop()
	var recoveryHold time.Time
	recoveryFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-recovery.C:
			if retry != nil || time.Now().Before(recoveryHold) || !s.brain.HostRecoveryDue() {
				continue
			}
			attempt = 0
			if err := s.reconcileBrainHostContinuity(); err != nil {
				recoveryFailures++
				recoveryHold = time.Now().Add(min(brainHostRecoveryInterval<<recoveryFailures, brainHostRecoveryMaxHold))
			} else {
				recoveryFailures, recoveryHold = 0, time.Time{}
			}
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
	// A failure, and the success that ends one, change the host problem the
	// snapshot carries even when the Host Session did not change.
	failed := err != nil
	if changed || failed || s.brainHostReconcileFailed.Swap(failed) {
		s.brainHostReconcileFailed.Store(failed)
		s.broadcastBrainHostCapabilityRefresh()
	}
	if err != nil {
		log.Printf("brain Host continuity reconciliation failed: %v", err)
	}
	return err
}
