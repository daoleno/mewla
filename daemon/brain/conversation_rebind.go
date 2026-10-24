package brain

import "sync"

// conversationRebinds tells Brain when a Worker's agent announced a different
// conversation (a /clear, a resume), so the identity Brain proved from the live
// process is re-proved then, not on a timer. A Worker the agent event Hub does
// not cover (no announcement to wait for) keeps the timed recheck.
type conversationRebinds struct {
	mu      sync.Mutex
	epoch   map[string]uint64
	covered func(workerID string) bool
}

func (r *conversationRebinds) note(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.epoch == nil {
		r.epoch = map[string]uint64{}
	}
	r.epoch[workerID]++
}

// state returns the Worker's rebind count and whether announcements cover it.
func (r *conversationRebinds) state(workerID string) (epoch uint64, covered bool) {
	r.mu.Lock()
	epoch, cover := r.epoch[workerID], r.covered
	r.mu.Unlock()
	return epoch, cover != nil && cover(workerID)
}

// NoteConversationRebound records that the Worker's agent announced a new
// conversation; the next reconcile re-proves its transcript.
func (s *Service) NoteConversationRebound(workerID string) {
	if s == nil {
		return
	}
	s.rebinds.note(workerID)
}

// SetConversationCoverage names the Workers whose conversation changes arrive
// as NoteConversationRebound calls.
func (s *Service) SetConversationCoverage(covered func(workerID string) bool) {
	if s == nil {
		return
	}
	s.rebinds.mu.Lock()
	s.rebinds.covered = covered
	s.rebinds.mu.Unlock()
}
