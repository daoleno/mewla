package brain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// A persistent loop is an until_done Work whose next check is a durable
// due_retry wake owned by the lifecycle engine. Mewla persists and delivers
// the wake to whichever Brain is the Host when it is due; Brain decides what
// the check found, when to check again (resolve with wait) and when the goal
// is met (resolve with complete). No provider-private timer (Claude /loop,
// CronCreate, ScheduleWakeup) is involved, so replacing the Host or restarting
// the daemon changes nothing about the loop.

// WorkLoop is the persistent part of a loop that the Work row does not
// already carry.
type WorkLoop struct {
	// IntervalSeconds, when positive, is the default gap to the next check: a
	// wait resolved without a time re-arms at now+interval. Brain may always
	// name an exact time instead.
	IntervalSeconds int64 `json:"interval_seconds,omitempty"`
	// WorkerExecutor, WorkerModel and WorkerReasoning are the route Brain
	// passes to `mewla worker spawn` when this loop delegates, whichever
	// executor Brain itself runs on. Empty model or reasoning keeps that
	// executor's default.
	WorkerExecutor  string `json:"worker_executor,omitempty"`
	WorkerModel     string `json:"worker_model,omitempty"`
	WorkerReasoning string `json:"worker_reasoning,omitempty"`
}

// LoopStart creates a loop. Objective and DoneCriteriaRef are the user's
// authorization and acceptance condition; the runtime never invents either.
type LoopStart struct {
	Title           string    `json:"title"`
	Objective       string    `json:"objective"`
	DoneCriteriaRef string    `json:"done_criteria_ref"`
	ContextRef      string    `json:"context_ref,omitempty"`
	Loop            WorkLoop  `json:"loop"`
	FirstCheckAt    time.Time `json:"first_check_at,omitempty"`
}

// Loop states reported by LoopStatus.
const (
	LoopScheduled = "scheduled" // a timed wake is armed
	LoopDue       = "due"       // the wake fired; waiting for the Host to take it
	LoopHandling  = "handling"  // the Host is handling the wake now
	LoopRunning   = "running"   // a delegated Attempt is active
	LoopPaused    = "paused"    // no timer; nothing fires until resumed
	LoopUnarmed   = "unarmed"   // not finished, not paused, but no next check set
	LoopDone      = "done"
	LoopCancelled = "cancelled"
)

// LoopStatus is the read model of one loop. The Work row stays the single
// authority: this derives from it and the lifecycle state.
type LoopStatus struct {
	Work        Work       `json:"work"`
	State       string     `json:"state"`
	NextCheckAt *time.Time `json:"next_check_at,omitempty"`
	LastSummary string     `json:"last_summary,omitempty"`
	LastHandled *time.Time `json:"last_handled_at,omitempty"`
	Checks      int        `json:"checks"`
}

var (
	ErrNotLoop       = errors.New("Work is not a persistent loop")
	ErrLoopHandling  = errors.New("a wake is being handled; resolve it with wait, complete or cancel first")
	ErrLoopTerminal  = errors.New("the loop is finished")
	ErrLoopAttempt   = errors.New("a delegated Attempt is active; let it finish first")
	maxLoopIntervalS = int64(366 * 24 * 3600)
)

func loopWakeRef(workID string) string { return "loop:" + strings.TrimSpace(workID) }

// pausedLoopWake reports the one persisted wake that has no time: a loop's
// own due_retry after PauseLoop. Brain cannot create it through a disposition
// (validateWorkWake still requires a time there).
func pausedLoopWake(item Work) bool {
	return item.Loop != nil && item.Wake != nil && item.Wake.Kind == WorkWakeDueRetry &&
		item.Wake.NextAttemptAt == nil && item.Wake.Ref == loopWakeRef(item.ID)
}

func validateWorkLoop(item Work) error {
	loop := item.Loop
	if loop == nil {
		return nil
	}
	if item.CompletionPolicy != CompletionUntilDone {
		return fmt.Errorf("a loop must be until_done")
	}
	if loop.IntervalSeconds < 0 || loop.IntervalSeconds > maxLoopIntervalS {
		return fmt.Errorf("loop interval must be between 0 and %d seconds", maxLoopIntervalS)
	}
	for _, value := range []string{loop.WorkerExecutor, loop.WorkerModel, loop.WorkerReasoning} {
		if strings.ContainsAny(value, "\x00\r\n") || !utf8.ValidString(value) {
			return fmt.Errorf("loop worker route must be single-line text")
		}
	}
	if strings.TrimSpace(loop.WorkerExecutor) == "" && (strings.TrimSpace(loop.WorkerModel) != "" || strings.TrimSpace(loop.WorkerReasoning) != "") {
		return fmt.Errorf("worker model and reasoning require a worker executor")
	}
	return nil
}

func normalizeLoop(loop WorkLoop) WorkLoop {
	loop.WorkerExecutor = strings.TrimSpace(loop.WorkerExecutor)
	loop.WorkerModel = strings.TrimSpace(loop.WorkerModel)
	loop.WorkerReasoning = strings.TrimSpace(loop.WorkerReasoning)
	return loop
}

// defaultLoopWake fills the time of a wait on a loop that names none, so a
// fixed cadence needs no arithmetic from Brain. An explicit time always wins.
func (s *Store) defaultLoopWake(request *WorkReviewDispositionRequest) {
	if request.Disposition != WorkDispositionWait || request.Wake == nil ||
		request.Wake.Kind != WorkWakeDueRetry || request.Wake.NextAttemptAt != nil {
		return
	}
	item, err := s.Work(request.WorkID)
	if err != nil || item.Loop == nil || item.Loop.IntervalSeconds <= 0 {
		return
	}
	next := s.nowUTC().Add(time.Duration(item.Loop.IntervalSeconds) * time.Second)
	wake := *request.Wake
	wake.NextAttemptAt = &next
	if strings.TrimSpace(wake.Ref) == "" {
		wake.Ref = loopWakeRef(item.ID)
	}
	request.Wake = &wake
}

// StartLoop creates the loop Work and arms its first check in one step. The
// Work freezes the current Brain thread, so results and wakes return there.
func (s *Store) StartLoop(in LoopStart) (Work, error) {
	loop := normalizeLoop(in.Loop)
	now := s.nowUTC()
	at := in.FirstCheckAt.UTC()
	if in.FirstCheckAt.IsZero() {
		if loop.IntervalSeconds <= 0 {
			return Work{}, fmt.Errorf("a loop needs a first check time or an interval")
		}
		at = now.Add(time.Duration(loop.IntervalSeconds) * time.Second)
	}
	if !at.After(now) {
		return Work{}, fmt.Errorf("the first check must be in the future")
	}
	nextAction := "Check progress toward the objective, then re-arm the next check or finish."
	if loop.WorkerExecutor != "" {
		nextAction += " Delegate with worker spawn -executor " + loop.WorkerExecutor
		if loop.WorkerModel != "" {
			nextAction += " -model " + loop.WorkerModel
		}
		if loop.WorkerReasoning != "" {
			nextAction += " -reasoning " + loop.WorkerReasoning
		}
		nextAction += "."
	}
	item, err := s.CreateWork(Work{
		Title: in.Title, Objective: in.Objective, CompletionPolicy: CompletionUntilDone,
		DoneCriteriaRef: in.DoneCriteriaRef, ContextRef: in.ContextRef, NextAction: nextAction, Loop: &loop,
	})
	if err != nil {
		return Work{}, err
	}
	if _, err := s.fsm.SetTimedWait(lifecycle.WorkID(item.ID), loopWakeRef(item.ID), &at); err != nil {
		// Do not leave a loop with no timer behind: it would read as unarmed.
		_, _ = s.CloseWork(WorkCloseRequest{WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled, Actor: "mewla", Reason: "loop start failed: " + err.Error()})
		return Work{}, err
	}
	return s.afterLoopChange(item.ID)
}

func (s *Store) afterLoopChange(workID string) (Work, error) {
	if err := s.SyncWorkProjection(workID); err != nil {
		return Work{}, err
	}
	s.broadcastWorkChange(workID)
	return s.Work(workID)
}

func (s *Store) loopWork(workID string) (Work, *lifecycle.State, error) {
	item, err := s.Work(workID)
	if err != nil {
		return Work{}, nil, err
	}
	if item.Loop == nil {
		return Work{}, nil, ErrNotLoop
	}
	st, err := s.fsmState(item.ID)
	if err != nil {
		return Work{}, nil, err
	}
	switch {
	case st.TerminalAt != nil:
		return item, st, ErrLoopTerminal
	case st.Attempt != nil:
		return item, st, ErrLoopAttempt
	case st.Review != nil:
		return item, st, ErrLoopHandling
	}
	return item, st, nil
}

// PauseLoop removes the timer but keeps the loop, its objective and history.
// Nothing fires until ResumeLoop. A pause is refused while a wake is being
// handled or a delegated Attempt runs: those finish through their own path.
func (s *Store) PauseLoop(workID string) (Work, error) {
	item, st, err := s.loopWork(workID)
	if err != nil {
		return item, err
	}
	if st.Wake != nil && st.Wake.Kind == lifecycle.WakeDueRetry && st.Wake.NextAttemptAt == nil {
		return item, nil // already paused
	}
	if _, err := s.fsm.SetTimedWait(lifecycle.WorkID(item.ID), loopWakeRef(item.ID), nil); err != nil {
		return Work{}, err
	}
	return s.afterLoopChange(item.ID)
}

// ResumeLoop arms the next check at `at`; a zero `at` uses the loop's
// interval from now. It also reschedules a loop that is already scheduled.
// Time spent paused or offline is not replayed: one wake is armed, from now.
func (s *Store) ResumeLoop(workID string, at time.Time) (Work, error) {
	item, _, err := s.loopWork(workID)
	if err != nil {
		return item, err
	}
	now := s.nowUTC()
	if at.IsZero() {
		if item.Loop.IntervalSeconds <= 0 {
			return Work{}, fmt.Errorf("name the next check time: this loop has no interval")
		}
		at = now.Add(time.Duration(item.Loop.IntervalSeconds) * time.Second)
	}
	at = at.UTC()
	if !at.After(now) {
		return Work{}, fmt.Errorf("the next check must be in the future")
	}
	if _, err := s.fsm.SetTimedWait(lifecycle.WorkID(item.ID), loopWakeRef(item.ID), &at); err != nil {
		return Work{}, err
	}
	return s.afterLoopChange(item.ID)
}

// Loops lists persistent loops, newest first. Finished ones are included only
// with includeFinished.
func (s *Store) Loops(includeFinished bool) ([]LoopStatus, error) {
	works, err := s.ListWork()
	if err != nil {
		return nil, err
	}
	var out []LoopStatus
	for _, item := range works {
		if item.Loop == nil {
			continue
		}
		status, err := s.loopStatus(item)
		if err != nil {
			return nil, err
		}
		if !includeFinished && (status.State == LoopDone || status.State == LoopCancelled) {
			continue
		}
		out = append(out, status)
	}
	return out, nil
}

// LoopStatus reports one loop.
func (s *Store) LoopStatus(workID string) (LoopStatus, error) {
	item, err := s.Work(workID)
	if err != nil {
		return LoopStatus{}, err
	}
	if item.Loop == nil {
		return LoopStatus{}, ErrNotLoop
	}
	return s.loopStatus(item)
}

func (s *Store) loopStatus(item Work) (LoopStatus, error) {
	st, err := s.fsmState(item.ID)
	if err != nil {
		return LoopStatus{}, err
	}
	status := LoopStatus{Work: item}
	switch {
	case st.TerminalAt != nil && item.Status == WorkCancelled:
		status.State = LoopCancelled
	case st.TerminalAt != nil:
		status.State = LoopDone
	case st.Attempt != nil:
		status.State = LoopRunning
	case st.Review != nil && st.Review.Handler != nil && st.Review.Handler.DeliveredAt != nil:
		status.State = LoopHandling
	case st.Review != nil:
		status.State = LoopDue
	case st.Wake != nil && st.Wake.Kind == lifecycle.WakeDueRetry && st.Wake.NextAttemptAt == nil:
		status.State = LoopPaused
	case st.Wake != nil && st.Wake.NextAttemptAt != nil:
		status.State = LoopScheduled
		next := st.Wake.NextAttemptAt.UTC()
		status.NextCheckAt = &next
	default:
		status.State = LoopUnarmed
	}
	events, err := s.ListWorkEvents(item.ID)
	if err != nil {
		return LoopStatus{}, err
	}
	for _, event := range events {
		if event.HandledAt == nil {
			continue
		}
		status.Checks++
		if status.LastHandled == nil || event.HandledAt.After(*status.LastHandled) {
			handled := *event.HandledAt
			status.LastHandled = &handled
			status.LastSummary = firstNonEmpty(event.DispositionSummary, event.Summary)
		}
	}
	return status, nil
}

// Service entry points: the control socket calls these, and each change is
// announced so the daemon's lifecycle timer re-reads the next due wake.

func (s *Service) StartLoop(in LoopStart) (Work, error) { return s.store.StartLoop(in) }

func (s *Service) PauseLoop(workID string) (Work, error) { return s.store.PauseLoop(workID) }

func (s *Service) ResumeLoop(workID string, at time.Time) (Work, error) {
	return s.store.ResumeLoop(workID, at)
}

func (s *Service) Loops(includeFinished bool) ([]LoopStatus, error) {
	return s.store.Loops(includeFinished)
}

func (s *Service) LoopStatus(workID string) (LoopStatus, error) { return s.store.LoopStatus(workID) }
