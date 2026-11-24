package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/classifier"
)

// A Session's runs, kept so the files a run named still open once it ended: the person asks an agent for
// a report and reads it later, from the Session's chat. Each live run (Session, process, start) is recorded
// with the folder it worked in; the file routes resolve an ended run's references against that folder,
// under the same rules as a live one. Bounded, newest kept, in the daemon's state.
const maxSessionRuns = 512

type sessionRun struct {
	WorkerID    string `json:"worker_id"`
	ProcessID   int    `json:"process_id"`
	StartedAtMS int64  `json:"started_at_ms"`
	Cwd         string `json:"cwd"`
	// Command is the run's launch command. Once the run ended the Worker keeps none, and its chat is
	// found by the conversation the command names (Claude's --session-id or --resume).
	Command string    `json:"command,omitempty"`
	SeenAt  time.Time `json:"seen_at"`
}

type sessionRuns struct {
	mu     sync.Mutex
	path   string
	loaded bool
	runs   []sessionRun // oldest first
}

func newSessionRuns(manager *auth.Manager) *sessionRuns {
	runs := &sessionRuns{}
	if manager != nil && manager.StorageDir() != "" {
		runs.path = filepath.Join(manager.StorageDir(), "session-runs.json")
	}
	return runs
}

// liveRun: the run a Worker is in now, if it has one (a process, its start and a folder).
func liveRun(worker *classifier.Worker) (sessionRun, bool) {
	if worker == nil || strings.TrimSpace(worker.ID) == "" || worker.ProcessID <= 0 || worker.StartedAt.IsZero() || strings.TrimSpace(worker.Cwd) == "" {
		return sessionRun{}, false
	}
	return sessionRun{WorkerID: strings.TrimSpace(worker.ID), ProcessID: worker.ProcessID, StartedAtMS: worker.StartedAt.UnixMilli(), Cwd: worker.Cwd, Command: strings.TrimSpace(worker.Command)}, true
}

// note records the Worker's live run, written only when the run or its folder is new.
func (r *sessionRuns) note(worker *classifier.Worker) {
	run, ok := liveRun(worker)
	if r == nil || !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	for i, known := range r.runs {
		if known.WorkerID == run.WorkerID && known.ProcessID == run.ProcessID && known.StartedAtMS == run.StartedAtMS {
			if known.Cwd == run.Cwd && (known.Command == run.Command || run.Command == "") {
				return
			}
			r.runs = append(r.runs[:i], r.runs[i+1:]...)
			break
		}
	}
	run.SeenAt = time.Now().UTC()
	r.runs = append(r.runs, run)
	if len(r.runs) > maxSessionRuns {
		r.runs = append([]sessionRun(nil), r.runs[len(r.runs)-maxSessionRuns:]...)
	}
	r.saveLocked()
}

// find: the recorded run of exactly this Session, process and start.
func (r *sessionRuns) find(workerID string, processID int, startedAtMS int64) (sessionRun, bool) {
	if r == nil {
		return sessionRun{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	for i := len(r.runs) - 1; i >= 0; i-- {
		run := r.runs[i]
		if run.WorkerID == strings.TrimSpace(workerID) && run.ProcessID == processID && run.StartedAtMS == startedAtMS {
			return run, true
		}
	}
	return sessionRun{}, false
}

// last: the Session's most recent recorded run.
func (r *sessionRuns) last(workerID string) (sessionRun, bool) {
	if r == nil {
		return sessionRun{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	for i := len(r.runs) - 1; i >= 0; i-- {
		if r.runs[i].WorkerID == strings.TrimSpace(workerID) {
			return r.runs[i], true
		}
	}
	return sessionRun{}, false
}

func (r *sessionRuns) loadLocked() {
	if r.loaded {
		return
	}
	r.loaded = true
	if r.path == "" {
		return
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var stored struct {
		Runs []sessionRun `json:"runs"`
	}
	if json.Unmarshal(raw, &stored) == nil {
		r.runs = stored.Runs
	}
}

func (r *sessionRuns) saveLocked() {
	if r.path == "" {
		return
	}
	raw, err := json.Marshal(struct {
		Runs []sessionRun `json:"runs"`
	}{r.runs})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return
	}
	_ = atomicfile.Write(r.path, raw, 0o600)
}

// sessionFileRunWire is the run a Session's files open through (`file_run` on its row): its live run, or
// once that ended, the last one recorded.
type sessionFileRunWire struct {
	ProcessID int   `json:"process_id"`
	StartedAt int64 `json:"started_at"`
}

func (s *Server) sessionFileRun(worker *classifier.Worker) *sessionFileRunWire {
	if run, ok := liveRun(worker); ok {
		s.sessionRuns.note(worker)
		return &sessionFileRunWire{ProcessID: run.ProcessID, StartedAt: run.StartedAtMS}
	}
	if worker == nil {
		return nil
	}
	if run, ok := s.sessionRuns.last(worker.ID); ok {
		return &sessionFileRunWire{ProcessID: run.ProcessID, StartedAt: run.StartedAtMS}
	}
	return nil
}

// endedRunWorker gives an ended Worker (no process, no command) what its last run was: its launch
// command and identity, so its chat binds to that run's own conversation, not to whichever
// conversation is newest in a folder other Sessions share.
func (s *Server) endedRunWorker(worker classifier.Worker) classifier.Worker {
	if s == nil || strings.TrimSpace(worker.Command) != "" || worker.ProcessID > 0 {
		return worker
	}
	run, ok := s.sessionRuns.last(worker.ID)
	if !ok || run.Command == "" {
		return worker
	}
	worker.Command = run.Command
	if worker.StartedAt.IsZero() {
		worker.StartedAt = time.UnixMilli(run.StartedAtMS)
	}
	if strings.TrimSpace(worker.Cwd) == "" {
		worker.Cwd = run.Cwd
	}
	return worker
}

// sessionFileWorker: the Session a file request names, as the run it names: the live run when it matches,
// else that run as recorded (an ended run, its folder), else the refusal a live check gives.
func (s *Server) sessionFileWorker(raw clientMessage) (*classifier.Worker, error) {
	workerID := strings.TrimSpace(raw.WorkerID)
	live := s.currentSessionFileWorker(workerID)
	err := validateSessionFileIdentity(live, raw)
	if err == nil {
		s.sessionRuns.note(live)
		return live, nil
	}
	if startedAt := clientStartedAt(raw.StartedAt); raw.ProcessID > 0 && !startedAt.IsZero() {
		if run, ok := s.sessionRuns.find(workerID, raw.ProcessID, startedAt.UnixMilli()); ok {
			return &classifier.Worker{ID: run.WorkerID, Cwd: run.Cwd, ProcessID: run.ProcessID, StartedAt: time.UnixMilli(run.StartedAtMS)}, nil
		}
	}
	return nil, err
}
