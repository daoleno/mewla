package server

import (
	"log"
	"time"
)

// Called only by the heartbeat lifecycle goroutine after watcher inventory is
// ready. Client snapshots and watcher projection events never launch a Host.
func (s *Server) reconcileBrainHostContinuity(now time.Time) {
	if s.brain == nil || now.Before(s.brainHostRetryAt) {
		return
	}
	changed, err := s.brain.ReconcileHostContinuity()
	if changed {
		s.broadcastBrainHostCapabilityRefresh()
	}
	if err != nil || changed {
		s.brainHostStableSince = time.Time{}
		if s.brainHostRetryDelay == 0 {
			s.brainHostRetryDelay = 10 * time.Second
		} else {
			s.brainHostRetryDelay = min(2*s.brainHostRetryDelay, time.Minute)
		}
		s.brainHostRetryAt = now.Add(s.brainHostRetryDelay)
		if err != nil {
			log.Printf("brain Host continuity reconciliation failed (retry in %s): %v", s.brainHostRetryDelay, err)
		}
		return
	}
	s.brainHostRetryAt = time.Time{}
	if s.brainHostStableSince.IsZero() {
		s.brainHostStableSince = now
	}
	// A successful launch can still immediately crash. Reset the backoff only
	// after the same Host has survived a full minute of healthy observations.
	if now.Sub(s.brainHostStableSince) >= time.Minute {
		s.brainHostRetryDelay = 0
	}
}
