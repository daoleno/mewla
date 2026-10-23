package calendar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
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

// ActionWaker is implemented by a runner that can say when a launched action
// may have made progress (its Work item changed). The scheduler reconciles
// running actions on that signal instead of re-reading them on a timer. The
// returned stop func ends the subscription.
type ActionWaker interface {
	ScheduledActionProgress() (progress <-chan struct{}, stop func())
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
	newTimer     func(time.Duration) wakeTimer
	missedWindow time.Duration
	launchMu     sync.Mutex
	launching    map[string]struct{}
	failures     logonce.Log
	wake         chan struct{}
	workerWake   chan struct{}
	passes       atomic.Int64
}

// safetyNet bounds how long the scheduler goes without looking at the store.
// Every wake point is derived from the store and the clock, so this is not what
// makes reminders fire. It covers a clock that jumped, a suspended machine
// (timers do not count suspended time) and a write that failed and is retried.
const safetyNet = time.Minute

type wakeTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type realTimer struct{ *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.Timer.C }

func newRealTimer(d time.Duration) wakeTimer { return realTimer{time.NewTimer(d)} }

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
	return &Scheduler{store: store, runner: runner, now: time.Now, newTimer: newRealTimer, wake: make(chan struct{}, 1), workerWake: make(chan struct{}, 1), missedWindow: DefaultMissedActionWindow, launching: map[string]struct{}{}}
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
	s.Wake()
}

// WorkerChanged tells Run that a Worker changed state or went away, which may
// end a launched action. Only running actions are re-checked. It never blocks
// and is safe on a nil Scheduler.
func (s *Scheduler) WorkerChanged() {
	if s == nil {
		return
	}
	select {
	case s.workerWake <- struct{}{}:
	default:
	}
}

// Wake asks Run to look at the store again. It never blocks and is safe on a
// nil Scheduler.
func (s *Scheduler) Wake() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) duty(owner string) (OwnerDuty, bool) {
	s.dutiesMu.Lock()
	defer s.dutiesMu.Unlock()
	duty, ok := s.duties[owner]
	return duty, ok && duty.Fire != nil
}

// wakePoints collects the earliest future instant at which a pass has
// something to do. Instants that are not in the future are ignored: a pass
// that could not act on an item yet (launching, backing off) is woken by the
// change that unblocks it, not by a past time.
type wakePoints struct {
	now time.Time
	at  time.Time
}

func (w *wakePoints) consider(t time.Time) {
	if t.After(w.now) && (w.at.IsZero() || t.Before(w.at)) {
		w.at = t
	}
}

// tickOwned acts on an owned reminder. Its owner acts instead of the
// reminder notification; it never waits for the user.
func (s *Scheduler) tickOwned(item Item, now time.Time, next *wakePoints) {
	duty, ok := s.duty(item.Owner)
	if !ok || item.Status != StatusScheduled {
		return
	}
	if item.NextAt.After(now) {
		next.consider(item.NextAt)
		return
	}
	if s.isLaunching(item.ID) {
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
	if wait.After(now) {
		next.consider(wait)
		return
	}
	if !s.beginLaunch(item.ID) {
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
			s.Wake() // the retry time is a new wake point
			return
		}
		_, _, err := s.store.CompleteOccurrence(item.ID, item.NextAt)
		s.note("calendar complete-occurrence "+item.ID, err)
	}()
}

// Run acts on due items until ctx ends. It sleeps on one timer armed for the
// earliest instant anything is due and looks again when the store changes, an
// owner registers, a launched action's Work progresses, or the safety net
// elapses; an idle scheduler does no work.
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	var progress <-chan struct{}
	if waker, ok := s.runner.(ActionWaker); ok {
		var stop func()
		progress, stop = waker.ScheduledActionProgress()
		defer stop()
	}
	changed := s.store.Changed()
	newTimer := s.newTimer
	if newTimer == nil {
		newTimer = newRealTimer
	}
	// Whatever changed before this point is seen by the first pass; only what
	// changes after it needs a wake.
	drain(changed)
	drain(s.wake)
	next := s.tick(ctx)
	for {
		wait := safetyNet
		if !next.IsZero() {
			wait = min(wait, next.Sub(s.now()))
		}
		timer := newTimer(max(wait, time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
			next = s.tick(ctx)
		case <-changed:
			next = s.tick(ctx)
		case <-s.wake:
			next = s.tick(ctx)
		case <-s.workerWake:
			s.reconcileRunning(ctx)
		case _, ok := <-progress:
			if !ok {
				progress = nil // the subscription ended; never spin on a closed channel
			} else {
				s.reconcileRunning(ctx)
			}
		}
		timer.Stop()
	}
}

func drain(ch <-chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

// Tick runs one full pass over every item.
func (s *Scheduler) Tick(ctx context.Context) {
	s.tick(ctx)
}

// tick acts on everything due and returns the earliest future instant that
// needs another pass, or the zero time when nothing is waiting on the clock.
func (s *Scheduler) tick(ctx context.Context) time.Time {
	if s == nil || s.store == nil {
		return time.Time{}
	}
	s.passes.Add(1)
	now := s.now()
	next := &wakePoints{now: now}
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
				s.tickOwned(item, now, next)
				continue
			}
			if item.Status != StatusScheduled {
				continue
			}
			if item.NextAt.After(now) {
				next.consider(item.NextAt)
				continue
			}
			status := StatusWaiting
			if item.Recurrence != RecurrenceNone {
				status = StatusCompleted
			}
			s.setStatus(item.ID, status)
		case KindEvent:
			if item.EndAt != nil {
				if !item.EndAt.After(now) {
					s.setStatus(item.ID, StatusCompleted)
					continue
				}
				next.consider(*item.EndAt)
			}
			if item.Status != StatusScheduled {
				continue
			}
			if item.NextAt.After(now) {
				next.consider(item.NextAt)
			} else {
				s.setStatus(item.ID, StatusRunning)
			}
		case KindScheduledAction:
			if item.Status != StatusScheduled {
				continue
			}
			if item.NextAt.After(now) {
				next.consider(item.NextAt)
				continue
			}
			if s.isLaunching(item.ID) {
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
	return next.at
}

// reconcileRunning re-checks launched actions against their Work. It runs when
// that Work may have progressed, without a pass over unrelated items.
func (s *Scheduler) reconcileRunning(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	for _, item := range s.store.Running() {
		if !s.isLaunching(item.ID) {
			s.reconcile(ctx, item)
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
