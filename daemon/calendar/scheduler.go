package calendar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/logonce"
)

const DefaultMissedActionWindow = 15 * time.Minute

type ActionResult struct {
	WorkID, WorkerSession string
	Launched              bool
}
type ActionRunner interface {
	RunScheduledAction(context.Context, Item, Run) (ActionResult, error)
}

// ActionInspector reconciles a launched calendar run with its canonical Work
// item/Worker lifecycle. A false known result means the linked execution can no
// longer be observed and must never be relaunched implicitly.
type ActionInspector interface {
	InspectScheduledAction(context.Context, Item, Run) (status Status, result, failure string, known bool)
}

// OwnerDuty is what an owner does when an item it owns is due. Fire gets the
// instant the occurrence was due. An occurrence more than Window late is
// skipped, not fired: the daemon was down and the moment has passed. A failed
// Fire is tried again until then.
type OwnerDuty struct {
	Window time.Duration
	Fire   func(item Item, due time.Time) error
}

// ownerRetry spaces the attempts after a failed Fire.
const ownerRetry = time.Minute

type Scheduler struct {
	store        *Store
	dutiesMu     sync.Mutex
	duties       map[string]OwnerDuty
	retryAt      map[string]time.Time
	runner       ActionRunner
	now          func() time.Time
	interval     time.Duration
	missedWindow time.Duration
	launchMu     sync.Mutex
	launching    map[string]struct{}
	failures     logonce.Log
}

// note reports a failed store write once per distinct cause and operation, so
// a completion that cannot be persisted is visible without a line every pass.
// A later success clears the scope. ErrClaimed is the benign loss of a launch
// race, not a failure.
func (s *Scheduler) note(scope string, err error) {
	if errors.Is(err, ErrClaimed) {
		err = nil
	}
	s.failures.Note(scope, err)
}

func NewScheduler(store *Store, runner ActionRunner) *Scheduler {
	return &Scheduler{store: store, runner: runner, now: time.Now, interval: time.Second, missedWindow: DefaultMissedActionWindow, launching: map[string]struct{}{}}
}

// Own registers what happens when an item owned by owner is due. Until it is
// registered such an item is left alone.
func (s *Scheduler) Own(owner string, duty OwnerDuty) {
	s.dutiesMu.Lock()
	defer s.dutiesMu.Unlock()
	if s.duties == nil {
		s.duties = map[string]OwnerDuty{}
	}
	s.duties[owner] = duty
}

func (s *Scheduler) duty(owner string) (OwnerDuty, bool) {
	s.dutiesMu.Lock()
	defer s.dutiesMu.Unlock()
	duty, ok := s.duties[owner]
	return duty, ok && duty.Fire != nil
}

// tickOwned acts on an owned reminder. Its owner acts instead of the
// reminder notification; it never waits for the user.
func (s *Scheduler) tickOwned(item Item, now time.Time) {
	duty, ok := s.duty(item.Owner)
	if !ok || item.NextAt.After(now) || item.Status != StatusScheduled || s.isLaunching(item.ID) {
		return
	}
	if now.Sub(item.NextAt) > duty.Window {
		_, _, err := s.store.CompleteOccurrence(item.ID, item.NextAt)
		s.note("calendar skip-missed "+item.ID, err)
		return
	}
	s.dutiesMu.Lock()
	wait := s.retryAt[item.ID]
	s.dutiesMu.Unlock()
	if wait.After(now) || !s.beginLaunch(item.ID) {
		return
	}
	go func() {
		defer s.endLaunch(item.ID)
		if err := duty.Fire(item, item.NextAt); err != nil {
			log.Printf("WARN calendar: %s %s: %v", item.Owner, item.ID, err)
			s.dutiesMu.Lock()
			if s.retryAt == nil {
				s.retryAt = map[string]time.Time{}
			}
			s.retryAt[item.ID] = now.Add(ownerRetry)
			s.dutiesMu.Unlock()
			return
		}
		_, _, err := s.store.CompleteOccurrence(item.ID, item.NextAt)
		s.note("calendar complete-occurrence "+item.ID, err)
	}()
}

func (s *Scheduler) Run(ctx context.Context) {
	s.Tick(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	now := s.now()
	for _, item := range s.store.List() {
		if item.Kind == KindScheduledAction && item.Status == StatusRunning {
			if !s.isLaunching(item.ID) {
				s.reconcile(ctx, item)
			}
			continue
		}
		if item.Status == StatusCancelled || item.Status == StatusCompleted || item.Status == StatusFailed {
			continue
		}
		switch item.Kind {
		case KindReminder, KindDeadline:
			if item.Owner != "" {
				s.tickOwned(item, now)
				continue
			}
			if !item.NextAt.After(now) && item.Status == StatusScheduled {
				status := StatusWaiting
				if item.Recurrence != RecurrenceNone {
					status = StatusCompleted
				}
				s.setStatus(item.ID, status)
			}
		case KindEvent:
			if item.EndAt != nil && !item.EndAt.After(now) {
				s.setStatus(item.ID, StatusCompleted)
			} else if !item.NextAt.After(now) && item.Status == StatusScheduled {
				s.setStatus(item.ID, StatusRunning)
			}
		case KindScheduledAction:
			if item.NextAt.After(now) || item.Status != StatusScheduled || s.isLaunching(item.ID) {
				continue
			}
			if now.Sub(item.NextAt) > s.missedWindow {
				reason := fmt.Sprintf("Missed while daemon was offline (more than %s late); use Run now to start it explicitly.", s.missedWindow)
				s.failOccurrence(ctx, item.ID, reason)
				continue
			}
			go func(id string) {
				_, err := s.run(ctx, id, false)
				s.note("calendar run "+id, err)
			}(item.ID)
		}
	}
}

func (s *Scheduler) RunNow(ctx context.Context, id string) (Item, error) {
	return s.run(ctx, id, true)
}

func (s *Scheduler) run(ctx context.Context, id string, manual bool) (Item, error) {
	if s == nil || s.store == nil {
		return Item{}, fmt.Errorf("calendar scheduler is not configured")
	}
	if !s.beginLaunch(id) {
		return Item{}, ErrClaimed
	}
	defer s.endLaunch(id)

	item, run, err := s.store.Claim(id, manual)
	if err != nil {
		return Item{}, err
	}
	if s.runner == nil {
		return s.store.FinishRun(id, run.ID, "", "scheduled action runner is not configured")
	}
	result, runErr := s.runner.RunScheduledAction(ctx, item, run)
	if runErr != nil {
		if result.Launched {
			return s.store.RecordLaunch(id, run.ID, result.WorkID, result.WorkerSession)
		}
		return s.complete(item, run, "", strings.TrimSpace(runErr.Error()))
	}
	// Launching visible Work is not task completion. Persist the link and leave
	// both the item and run running until reconciliation observes a terminal Work state.
	return s.store.RecordLaunch(id, run.ID, result.WorkID, result.WorkerSession)
}

func (s *Scheduler) failOccurrence(ctx context.Context, id, failure string) {
	item, run, err := s.store.Claim(id, false)
	if err != nil {
		s.note("calendar claim-missed "+id, err)
		return
	}
	s.finish(item, run, "", failure)
}

// setStatus moves an item and reports a failed write.
func (s *Scheduler) setStatus(id string, status Status) {
	_, err := s.store.SetStatus(id, status, "")
	s.note("calendar set-status "+id, err)
}

// finish ends a run and reports a terminal state that could not be persisted.
func (s *Scheduler) finish(item Item, run Run, result, failure string) {
	_, err := s.complete(item, run, result, failure)
	s.note("calendar finish-run "+item.ID, err)
}

func (s *Scheduler) complete(item Item, run Run, result, failure string) (Item, error) {
	return s.store.FinishRun(item.ID, run.ID, result, failure)
}

func (s *Scheduler) reconcile(ctx context.Context, item Item) {
	var active *Run
	for idx := len(item.Runs) - 1; idx >= 0; idx-- {
		if item.Runs[idx].Status == StatusRunning {
			active = &item.Runs[idx]
			break
		}
	}
	if active == nil {
		_, err := s.store.SetStatus(item.ID, StatusFailed, "Running calendar item has no durable run record.")
		s.note("calendar set-status "+item.ID, err)
		return
	}
	inspector, ok := s.runner.(ActionInspector)
	if !ok {
		return
	} // Explicitly remain running when this architecture cannot prove a terminal state.
	status, result, failure, known := inspector.InspectScheduledAction(ctx, item, *active)
	if !known {
		s.finish(item, *active, "", "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution.")
		return
	}
	switch status {
	case StatusCompleted:
		s.finish(item, *active, result, "")
	case StatusFailed:
		if strings.TrimSpace(failure) == "" {
			failure = "Linked Work/agent failed."
		}
		s.finish(item, *active, "", failure)
	}
}

func (s *Scheduler) beginLaunch(id string) bool {
	if s == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	if s.launching == nil {
		s.launching = map[string]struct{}{}
	}
	if _, exists := s.launching[id]; exists {
		return false
	}
	s.launching[id] = struct{}{}
	return true
}

func (s *Scheduler) endLaunch(id string) {
	if s == nil {
		return
	}
	s.launchMu.Lock()
	delete(s.launching, strings.TrimSpace(id))
	s.launchMu.Unlock()
}

func (s *Scheduler) isLaunching(id string) bool {
	if s == nil {
		return false
	}
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	_, exists := s.launching[strings.TrimSpace(id)]
	return exists
}
