package server

import (
	"context"
	"log"
)

// Restore once after initial discovery, then only when the watcher reports
// removal of the current Host. There is no periodic probe or retry timer.
// Keeping recovery outside broadcastEvents lets discovery/output events drain
// while a replacement waits for provider readiness.
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
	s.reconcileBrainHostContinuity()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.brainHostRecovery:
			if ctx.Err() != nil {
				return
			}
			s.reconcileBrainHostContinuity()
		}
	}
}

func (s *Server) reconcileBrainHostContinuity() {
	if s.brain == nil {
		return
	}
	changed, err := s.brain.ReconcileHostContinuity()
	if changed {
		s.broadcastBrainHostCapabilityRefresh()
	}
	if err != nil {
		log.Printf("brain Host continuity reconciliation failed: %v", err)
	}
}
