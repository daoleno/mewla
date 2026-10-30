package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/push"
	"github.com/daoleno/mewla/daemon/watcher"
)

// One question, one push. A Worker's blocked or failed push says "this needs
// you"; sending it again for the same question only stacks copies on the
// phone. The question is the prompt's identity (its id, which survives the
// countdown and re-renders of its dialog), or for a state with no prompt the
// turn and what the summary says.
//
// The record survives a daemon restart, so a hot reload that rediscovers the
// same dialog does not ask again. A Worker that moved on (left the state and
// stayed away) is asked about afresh, so the next question always goes out.

const (
	// workerPushMovedOn is how long a Worker must stay out of blocked or failed
	// before the same question counts as a new one. A blink of the state
	// (running for a poll or two) is far shorter.
	workerPushMovedOn = 30 * time.Second

	reasonSameQuestion = "same_question_already_notified"
)

// workerPush is the last blocked or failed push for one Worker.
type workerPush struct {
	Kind   string    `json:"kind"`
	Key    string    `json:"key"`
	SentAt time.Time `json:"sent_at"`
	// LeftAt is when the Worker first left the state after the push; zero while
	// it is still in it. Kept in memory only: after a restart the Worker is
	// taken to be still there.
	LeftAt time.Time `json:"-"`
	// noted is whether Watch was already told this question's later pushes are
	// held back, so a blinking dialog leaves one record, not one per blink.
	noted bool
}

func (s *Server) workerPushTime() time.Time {
	if s.workerPushNow != nil {
		return s.workerPushNow()
	}
	return time.Now()
}

// workerPushesLocked returns the record of the last pushes, loading it on first
// use. The caller holds s.mu.
func (s *Server) workerPushesLocked() map[string]*workerPush {
	if s.workerPushed != nil {
		return s.workerPushed
	}
	s.workerPushed = make(map[string]*workerPush)
	if s.workerPushPath == "" {
		return s.workerPushed
	}
	raw, err := os.ReadFile(s.workerPushPath)
	if err != nil {
		return s.workerPushed
	}
	_ = json.Unmarshal(raw, &s.workerPushed)
	if s.workerPushed == nil {
		s.workerPushed = make(map[string]*workerPush)
	}
	return s.workerPushed
}

func (s *Server) saveWorkerPushesLocked() {
	if s.workerPushPath == "" {
		return
	}
	raw, err := json.Marshal(s.workerPushed)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.workerPushPath), 0o700); err != nil {
		return
	}
	_ = atomicfile.Write(s.workerPushPath, raw, 0o600)
}

// noteWorkerMoved hears that a Worker is no longer blocked or failed.
func (s *Server) noteWorkerMoved(ev watcher.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record := s.workerPushesLocked()[ev.WorkerID]; record != nil && record.LeftAt.IsZero() {
		record.LeftAt = s.workerPushTime()
	}
}

// workerPushSuppression says why a blocked or failed push should not go out
// because the person was already told this question, or "" when it should. A
// push that goes out is recorded as the Worker's last.
func (s *Server) workerPushSuppression(ev watcher.SessionEvent, kind string) (reason string, firstTime bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushed := s.workerPushesLocked()
	if kind == push.KindWorkerDone {
		// Settled: whatever was asked is over.
		if _, ok := pushed[ev.WorkerID]; ok {
			delete(pushed, ev.WorkerID)
			s.saveWorkerPushesLocked()
		}
		return "", false
	}
	now := s.workerPushTime()
	key := workerPushKey(ev)
	if record := pushed[ev.WorkerID]; record != nil && record.Kind == kind && record.Key == key &&
		(record.LeftAt.IsZero() || now.Sub(record.LeftAt) < workerPushMovedOn) {
		record.LeftAt = time.Time{}
		first := !record.noted
		record.noted = true
		return reasonSameQuestion, first
	}
	pushed[ev.WorkerID] = &workerPush{Kind: kind, Key: key, SentAt: now}
	s.saveWorkerPushesLocked()
	return "", false
}

// workerPushKey is the question a Worker's blocked or failed event is about.
func workerPushKey(ev watcher.SessionEvent) string {
	worker := ev.Worker
	if worker == nil {
		return "turn:" + ev.TurnID
	}
	if worker.Prompt != nil && worker.Prompt.ID != "" {
		return "prompt:" + worker.Prompt.ID
	}
	if worker.DetailsJSON != "" {
		var details struct {
			Prompt struct {
				ID string `json:"id"`
			} `json:"prompt"`
		}
		if json.Unmarshal([]byte(worker.DetailsJSON), &details) == nil && details.Prompt.ID != "" {
			return "prompt:" + details.Prompt.ID
		}
	}
	return "turn:" + ev.TurnID + "|" + strings.TrimSpace(worker.Summary)
}

// forgetWorkerPush drops the record of a push that reached no phone.
func (s *Server) forgetWorkerPush(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workerPushesLocked()[workerID]; ok {
		delete(s.workerPushed, workerID)
		s.saveWorkerPushesLocked()
	}
}
