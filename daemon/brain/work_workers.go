package brain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// WorkWorkerRecord is what a Worker Session was when it joined a Work: the
// attributes that die with its pane (its name, the executor it ran). Which
// Sessions joined a Work is not decided here: the Work's admissions and turns
// say that (workWorkerSessions). Records are append-only facts in
// state/work_workers.jsonl, one per Work and Session.
type WorkWorkerRecord struct {
	WorkID     string    `json:"work_id"`
	SessionID  string    `json:"session_id"`
	Name       string    `json:"name,omitempty"`
	ExecutorID string    `json:"executor_id,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	JoinedAt   time.Time `json:"joined_at"`
}

// WorkWorkerState is where one Worker Session of a Work stands.
type WorkWorkerState string

const (
	// The Session is open and running, or blocked on a prompt, for this Work.
	WorkWorkerRunning WorkWorkerState = "running"
	WorkWorkerBlocked WorkWorkerState = "blocked"
	// Its turn for this Work reported a result, or failed.
	WorkWorkerDone   WorkWorkerState = "done"
	WorkWorkerFailed WorkWorkerState = "failed"
	// The Session is gone (or moved on to other Work) without a reported
	// result for this one: stopped, closed, or lost.
	WorkWorkerEnded WorkWorkerState = "ended"
)

// WorkWorkerSession is one Worker Session of a Work, live or finished, as the
// Work card and detail list it.
type WorkWorkerSession struct {
	SessionID  string          `json:"session_id"`
	Name       string          `json:"name,omitempty"`
	ExecutorID string          `json:"executor_id,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	State      WorkWorkerState `json:"state"`
	// Summary is the Worker's newest reported line: what it is doing while
	// live, its result line once finished.
	Summary string `json:"summary,omitempty"`
	// Phase is the live Worker's reported phase; empty once finished.
	Phase string `json:"phase,omitempty"`
	// Live is true while the Session is open and working on this Work; the
	// client opens it and follows it on the Session list by session_id.
	Live      bool       `json:"live"`
	JoinedAt  time.Time  `json:"joined_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (s *Store) workWorkersPath() string {
	return filepath.Join(s.statePath(), "work_workers.jsonl")
}

func workWorkerKey(workID, sessionID string) string {
	return workID + "\x00" + sessionID
}

// RecordWorkWorker keeps what a Session was when it joined a Work. The first
// record for a Work and Session stands; later ones only fill fields it lacked
// (a Session first seen by the reconcile tick, then named by its spawn, never
// swaps its name). It reports whether a row was written.
func (s *Store) RecordWorkWorker(record WorkWorkerRecord) (bool, error) {
	record.WorkID = strings.TrimSpace(record.WorkID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Name = strings.TrimSpace(record.Name)
	record.ExecutorID = strings.TrimSpace(record.ExecutorID)
	record.Provider = strings.TrimSpace(record.Provider)
	record.Cwd = strings.TrimSpace(record.Cwd)
	if s == nil || record.WorkID == "" || record.SessionID == "" {
		return false, nil
	}
	if record.JoinedAt.IsZero() {
		record.JoinedAt = s.nowUTC()
	}
	record.JoinedAt = record.JoinedAt.UTC()
	s.workWorkersMu.Lock()
	defer s.workWorkersMu.Unlock()
	records, err := s.workWorkersLocked()
	if err != nil {
		return false, err
	}
	key := workWorkerKey(record.WorkID, record.SessionID)
	merged := record
	if existing, ok := records[key]; ok {
		merged = existing
		if merged.Name == "" {
			merged.Name = record.Name
		}
		if merged.ExecutorID == "" {
			merged.ExecutorID = record.ExecutorID
		}
		if merged.Provider == "" {
			merged.Provider = record.Provider
		}
		if merged.Cwd == "" {
			merged.Cwd = record.Cwd
		}
		if merged == existing {
			return false, nil
		}
	}
	if err := appendJSONLine(s.workWorkersPath(), merged); err != nil {
		return false, err
	}
	records[key] = merged
	return true, nil
}

// workWorkerRecords returns the records of these Works by Work and Session.
func (s *Store) workWorkerRecords() (map[string]WorkWorkerRecord, error) {
	s.workWorkersMu.Lock()
	defer s.workWorkersMu.Unlock()
	return s.workWorkersLocked()
}

// workWorkersLocked loads the file once; this Store is its only writer, so
// later reads are the in-memory copy. A later row for the same Work and
// Session replaces the earlier one (RecordWorkWorker only appends merges).
func (s *Store) workWorkersLocked() (map[string]WorkWorkerRecord, error) {
	if s.workWorkers != nil {
		return s.workWorkers, nil
	}
	records := map[string]WorkWorkerRecord{}
	raw, err := os.ReadFile(s.workWorkersPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var record WorkWorkerRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.WorkID == "" || record.SessionID == "" {
			continue
		}
		records[workWorkerKey(record.WorkID, record.SessionID)] = record
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	s.workWorkers = records
	return records, nil
}

// workWorkerIndex is one pass over the turn ledger for a projection: each
// Session's newest turn, and each Work's delegated (non-host) turns.
type workWorkerIndex struct {
	latestBySession map[string]TurnRecord
	turnsByWork     map[string][]TurnRecord
	records         map[string]WorkWorkerRecord
	live            map[string]WorkerRef
	// hostID is Brain's own Session: never one of a Work's Workers.
	hostID string
}

func newWorkWorkerIndex(database presentationDatabase, records map[string]WorkWorkerRecord, workers []WorkerRef, hostID string) workWorkerIndex {
	index := workWorkerIndex{
		hostID:          strings.TrimSpace(hostID),
		latestBySession: map[string]TurnRecord{},
		turnsByWork:     map[string][]TurnRecord{},
		records:         records,
		live:            map[string]WorkerRef{},
	}
	for _, turn := range database.BrainTurns {
		if best, ok := index.latestBySession[turn.SessionID]; !ok || turn.AcceptedAt.After(best.AcceptedAt) ||
			(turn.AcceptedAt.Equal(best.AcceptedAt) && turn.TurnID > best.TurnID) {
			index.latestBySession[turn.SessionID] = turn
		}
		if isHostHandlingTurn(turn) || turn.WorkID == "" {
			continue
		}
		index.turnsByWork[turn.WorkID] = append(index.turnsByWork[turn.WorkID], turn)
	}
	for _, worker := range workers {
		if id := strings.TrimSpace(worker.ID); id != "" && !worker.Hidden {
			index.live[id] = worker
		}
	}
	return index
}

// workWorkerSessions lists every Worker Session that joined a Work, in the
// order they joined. Membership comes only from the Work's own durable facts:
// its admissions (a Session given the Work's prompt or Brain's follow-up to
// it, accepted or still settling) and its delegated turns. A record only names a member; a
// Session with no record is listed without a name, never hidden.
func (index workWorkerIndex) workWorkerSessions(workID string, st *lifecycle.State) []WorkWorkerSession {
	type member struct {
		joinedAt time.Time
		turn     TurnRecord
		hasTurn  bool
	}
	members := map[string]*member{}
	join := func(sessionID string, at time.Time) *member {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" || sessionID == index.hostID {
			return nil
		}
		m := members[sessionID]
		if m == nil {
			m = &member{joinedAt: at}
			members[sessionID] = m
		} else if !at.IsZero() && (m.joinedAt.IsZero() || at.Before(m.joinedAt)) {
			m.joinedAt = at
		}
		return m
	}
	if st != nil {
		for _, admission := range st.Admissions {
			if admission == nil || admission.Status == lifecycle.AdmissionAborted {
				continue
			}
			join(admission.SessionID, admission.AttemptedAt)
		}
	}
	for _, turn := range index.turnsByWork[workID] {
		at := turn.AcceptedAt
		if at.IsZero() {
			at = turn.UpdatedAt
		}
		m := join(turn.SessionID, at)
		if m == nil {
			continue
		}
		if !m.hasTurn || turn.AcceptedAt.After(m.turn.AcceptedAt) ||
			(turn.AcceptedAt.Equal(m.turn.AcceptedAt) && turn.TurnID > m.turn.TurnID) {
			m.turn, m.hasTurn = turn, true
		}
	}
	if len(members) == 0 {
		return nil
	}
	out := make([]WorkWorkerSession, 0, len(members))
	for sessionID, m := range members {
		record, hasRecord := index.records[workWorkerKey(workID, sessionID)]
		joinedAt := m.joinedAt
		if hasRecord && (joinedAt.IsZero() || record.JoinedAt.Before(joinedAt)) {
			joinedAt = record.JoinedAt
		}
		row := WorkWorkerSession{
			SessionID:  sessionID,
			Name:       record.Name,
			ExecutorID: record.ExecutorID,
			Provider:   record.Provider,
			JoinedAt:   joinedAt.UTC(),
			UpdatedAt:  joinedAt.UTC(),
		}
		live, isLive := index.live[sessionID]
		if isLive {
			// A reused Session that has since taken another Work's turn is
			// finished for this one; its live state is the other Work's.
			if latest, ok := index.latestBySession[sessionID]; ok && !isHostHandlingTurn(latest) && latest.WorkID != workID {
				isLive = false
			}
		}
		if isLive {
			if name := strings.TrimSpace(live.Name); name != "" {
				row.Name = name
			}
			if live.ExecutorID != "" {
				row.ExecutorID = live.ExecutorID
			}
			if live.Provider != "" {
				row.Provider = live.Provider
			}
			row.Live = true
			row.State = liveWorkWorkerState(live.Status)
			row.Summary = strings.TrimSpace(live.Summary)
			row.Phase = strings.TrimSpace(live.Phase)
			if !live.Updated.IsZero() {
				row.UpdatedAt = live.Updated.UTC()
			}
			if row.State == WorkWorkerDone || row.State == WorkWorkerFailed {
				// The pane is still open after its turn ended: the turn's
				// settle time is when this Worker finished.
				row.EndedAt = turnEndedAt(m.turn, m.hasTurn)
			}
		} else {
			row.State = WorkWorkerEnded
			if m.hasTurn {
				switch m.turn.Status {
				case watcher.TurnDone:
					row.State = WorkWorkerDone
				case watcher.TurnFailed:
					row.State = WorkWorkerFailed
				}
				row.Summary = strings.TrimSpace(m.turn.Summary)
				if !m.turn.UpdatedAt.IsZero() {
					row.UpdatedAt = m.turn.UpdatedAt.UTC()
				}
			}
			row.EndedAt = turnEndedAt(m.turn, m.hasTurn)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].JoinedAt.Equal(out[right].JoinedAt) {
			return out[left].SessionID < out[right].SessionID
		}
		return out[left].JoinedAt.Before(out[right].JoinedAt)
	})
	return out
}

func turnEndedAt(turn TurnRecord, ok bool) *time.Time {
	if !ok {
		return nil
	}
	if turn.SettledAt != nil {
		at := turn.SettledAt.UTC()
		return &at
	}
	if turn.UpdatedAt.IsZero() {
		return nil
	}
	at := turn.UpdatedAt.UTC()
	return &at
}

func liveWorkWorkerState(status string) WorkWorkerState {
	switch status {
	case "blocked":
		return WorkWorkerBlocked
	case "done":
		return WorkWorkerDone
	case "failed":
		return WorkWorkerFailed
	default:
		return WorkWorkerRunning
	}
}

// WorkDetail is one Work as its detail page reads it, current or closed: a
// closed Work is in neither current_work nor open_work, but its page still
// lists who worked on it.
type WorkDetail struct {
	ID             string              `json:"work_id"`
	Revision       uint64              `json:"revision"`
	Title          string              `json:"title"`
	Status         WorkStatus          `json:"status"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	WorkerSessions []WorkWorkerSession `json:"worker_sessions"`
	// ResultActions are the next steps of the Work's newest result, as its
	// result row carries them (work_result_actions.go); [] when none.
	ResultActions []work.ResultAction `json:"result_actions"`
}

// WorkDetail projects one Work. workers is a fresh Session inventory, as for
// ProjectWorkInventory.
func (s *Store) WorkDetail(workID string, workers []WorkerRef) (WorkDetail, error) {
	workID = strings.TrimSpace(workID)
	records, err := s.workWorkerRecords()
	if err != nil {
		return WorkDetail{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return WorkDetail{}, err
	}
	index := workIndex(database.BrainWork, workID)
	if workID == "" || index < 0 {
		return WorkDetail{}, ErrWorkNotFound
	}
	item := database.BrainWork[index]
	host, err := s.readHostSessionLocked()
	if err != nil {
		return WorkDetail{}, err
	}
	st, err := s.fsmState(item.ID)
	if err != nil {
		st = nil
	}
	sessions := newWorkWorkerIndex(database, records, workers, host.ID).workWorkerSessions(item.ID, st)
	if sessions == nil {
		sessions = []WorkWorkerSession{}
	}
	return WorkDetail{
		ID: item.ID, Revision: item.Revision, Title: item.Title, Status: item.Status,
		CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(), WorkerSessions: sessions,
		ResultActions: []work.ResultAction{},
	}, nil
}
