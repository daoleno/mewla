package calendar

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// heldRunner is a scheduled-action runner whose agent the provider refuses
// until refused is cleared.
type heldRunner struct {
	alive     bool
	refused   bool
	activity  string
	answered  []string // Activities the agent ran that were answered
	mayAnswer bool
	continues int
	launches  int
}

func (r *heldRunner) RunScheduledAction(_ context.Context, _ Item, run Run) (ActionResult, error) {
	r.launches++
	r.alive, r.refused, r.activity, r.answered = true, false, "", nil
	return ActionResult{WorkID: run.ID, WorkerSession: "mh-relaunched", Launched: true}, nil
}

func (r *heldRunner) InspectScheduledAction(_ context.Context, _ Item, _ Run) (Status, string, string, bool) {
	if !r.alive {
		return StatusRunning, "", "", false
	}
	if r.refused {
		// An agent the provider refused sits idle at its prompt: the inspector
		// alone would call that "finished without writing its result".
		return StatusFailed, "", "The agent finished without writing its result.", true
	}
	return StatusRunning, "", "", true
}

func (r *heldRunner) ScheduledActionRefused(_ Item, _ Run) (ProviderRefusal, bool, bool) {
	if !r.alive || !r.refused {
		return ProviderRefusal{}, r.alive, false
	}
	return ProviderRefusal{
		ActivityID: r.activity, Provider: "claude", Kind: "rate limit", At: time.Date(2026, 11, 21, 0, 0, 2, 0, time.UTC),
		Activities: append(append([]string(nil), r.answered...), r.activity),
	}, true, true
}

func (r *heldRunner) ProviderMayAnswer(string, time.Time) bool { return r.mayAnswer }

func (r *heldRunner) ContinueScheduledAction(Item, Run) error {
	r.continues++
	return nil
}

// heldFixture launches the 08:00 daily briefing whose agent the provider
// refuses at its first request (2026-11-21, Work calendar-612384f7…).
func heldFixture(t *testing.T) (*Store, *Scheduler, *heldRunner, *time.Time, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	due := now.In(mustLocation("Asia/Shanghai"))
	item, err := store.Create(Item{
		ID: "briefing", Title: "Daily Hacker News Briefing", Kind: KindScheduledAction, DueAt: &due,
		Timezone: "Asia/Shanghai", Recurrence: RecurrenceDaily, ActionInstruction: "Brief me", SourceThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &heldRunner{alive: true}
	scheduler := NewScheduler(store, runner)
	scheduler.now = func() time.Time { return now }
	if _, err := scheduler.run(context.Background(), item.ID, false); err != nil {
		t.Fatal(err)
	}
	runner.refused, runner.activity = true, "activity-1"
	return store, scheduler, runner, &now, item.ID
}

func activeRun(t *testing.T, store *Store, id string) (Item, Run) {
	t.Helper()
	item, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Runs) == 0 {
		t.Fatal("no run")
	}
	return item, item.Runs[len(item.Runs)-1]
}

func TestARunWhoseAgentTheProviderRefusedIsHeldAndContinuedOnceItAnswers(t *testing.T) {
	store, scheduler, runner, now, id := heldFixture(t)
	*now = now.Add(time.Minute)
	scheduler.Tick(context.Background())
	item, run := activeRun(t, store, id)
	if item.Status != StatusRunning || run.Status != StatusRunning || run.Held == nil || !run.Held.NoWork || run.Held.Kind != "rate limit" {
		t.Fatalf("a refused run was not held: item %s run %+v", item.Status, run)
	}
	if runner.continues != 0 {
		t.Fatal("continued while the provider refuses requests")
	}
	// An hour later the provider answers (or the family's probe is this run's).
	*now = now.Add(time.Hour)
	runner.mayAnswer = true
	scheduler.Tick(context.Background())
	scheduler.Tick(context.Background())
	if runner.continues != 1 {
		t.Fatalf("continues = %d, want exactly one per refusal", runner.continues)
	}
	// The continuation was answered: the run is running again, no longer held.
	runner.refused, runner.answered = false, []string{"activity-1", "activity-2"}
	scheduler.Tick(context.Background())
	if _, run := activeRun(t, store, id); run.Status != StatusRunning || run.Held != nil {
		t.Fatalf("after the provider answered: %+v", run)
	}
}

func TestAHeldRunWhoseSessionEndedStartsAgainOnlyIfItDidNoWork(t *testing.T) {
	store, scheduler, runner, now, id := heldFixture(t)
	scheduler.Tick(context.Background())
	runner.alive = false // its Session went away (2026-11-21 01:16:20)
	*now = now.Add(time.Hour)
	scheduler.Tick(context.Background())
	if _, run := activeRun(t, store, id); run.Status != StatusRunning || runner.launches != 1 {
		t.Fatalf("held while the provider refuses: run %+v launches %d", run, runner.launches)
	}
	runner.mayAnswer = true
	scheduler.Tick(context.Background())
	item, run := activeRun(t, store, id)
	if runner.launches != 2 || run.Status != StatusRunning || run.WorkerSession != "mh-relaunched" || len(item.Runs) != 1 {
		t.Fatalf("a run that did no work was not started again in place: launches %d run %+v", runner.launches, run)
	}

	// An agent that had an Activity answered may have acted: never twice.
	store, scheduler, runner, now, id = heldFixture(t)
	runner.answered = []string{"activity-0"}
	scheduler.Tick(context.Background())
	runner.alive, runner.mayAnswer = false, true
	*now = now.Add(time.Hour)
	scheduler.Tick(context.Background())
	if _, run := activeRun(t, store, id); run.Status != StatusFailed || runner.launches != 1 ||
		!strings.Contains(run.FailureReason, "after the agent had started working") {
		t.Fatalf("a run that had started was replayed or kept: launches %d run %+v", runner.launches, run)
	}
}

func TestAHeldRunIsNotMadeUpAfterItsWindow(t *testing.T) {
	store, scheduler, _, now, id := heldFixture(t)
	scheduler.Tick(context.Background())
	*now = now.Add(heldRunWindow + time.Minute)
	scheduler.Tick(context.Background())
	item, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	run := item.Runs[0]
	if run.Status != StatusFailed || !strings.Contains(run.FailureReason, "Not run: Claude's model provider refused requests (rate limit) from Nov 21 08:00 until Nov 21 20:00") {
		t.Fatalf("expired held run = %+v", run)
	}
	if item.Status != StatusScheduled || !item.NextAt.After(*now) {
		t.Fatalf("the series did not go on to its next occurrence: %+v", item)
	}
}

func TestRunNowReplacesAHeldRunAndRunsAFailedOccurrenceAgain(t *testing.T) {
	store, scheduler, runner, now, id := heldFixture(t)
	scheduler.Tick(context.Background())
	*now = now.Add(30 * time.Minute)
	runner.refused = false
	item, err := scheduler.RunNow(context.Background(), id)
	if err != nil {
		t.Fatalf("Run now on a held run: %v", err)
	}
	if len(item.Runs) != 2 || item.Runs[0].Status != StatusFailed || !strings.Contains(item.Runs[0].FailureReason, "Replaced by Run now") ||
		item.Runs[1].Status != StatusRunning || !item.Runs[1].Manual {
		t.Fatalf("runs after Run now = %+v", item.Runs)
	}
	// A failed occurrence can be run again by hand.
	if _, err := store.FinishRun(id, item.Runs[1].ID, "", "the agent stopped"); err != nil {
		t.Fatal(err)
	}
	again, err := scheduler.RunNow(context.Background(), id)
	if err != nil || len(again.Runs) != 3 || again.Runs[2].Status != StatusRunning {
		t.Fatalf("Run now after a failure: %v %+v", err, again.Runs)
	}
}

// heldWatcher is the watcher a WorkRunner reads a refused agent from.
type heldWatcher struct {
	observation watcher.ProviderActivityObservation
	typed       []string
}

func (w *heldWatcher) GetWorker(id string) *classifier.Worker {
	return &classifier.Worker{ID: id, Command: "claude --model sonnet", State: classifier.StateDone}
}
func (w *heldWatcher) HasSession(string) bool { return true }
func (w *heldWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return w.observation, true, nil
}
func (w *heldWatcher) SendInputWhenReady(_, _, text string) error {
	w.typed = append(w.typed, text)
	return nil
}

func TestWorkRunnerReadsARefusalFromTheAgentsOwnProviderRecord(t *testing.T) {
	w := &heldWatcher{observation: watcher.ProviderActivityObservation{
		ID: "a1", Status: "completed", ProbeState: watcher.ProbeStateOK, SettledAt: time.Now(),
		Error:              &watcher.ProviderActivityError{Code: "rate_limit", HTTPStatus: 429, Message: "Request rejected (429)"},
		TerminalActivities: []watcher.ProviderTerminalActivity{{ID: "a1", Status: "completed"}},
	}}
	runner := &WorkRunner{Watcher: w}
	run := Run{WorkerSession: "mh-briefing"}
	refusal, alive, refused := runner.ScheduledActionRefused(Item{}, run)
	if !alive || !refused || refusal.Provider != "claude" || refusal.Kind != "rate limit" || refusal.ActivityID != "a1" ||
		len(refusal.Activities) != 1 {
		t.Fatalf("refusal = %+v alive %v refused %v", refusal, alive, refused)
	}
	if !runner.ProviderMayAnswer("claude", time.Now()) {
		t.Fatal("a watcher with no availability state must not hold the run")
	}
	if err := runner.ContinueScheduledAction(Item{}, run); err != nil || len(w.typed) != 1 || w.typed[0] != watcher.TurnContinuationPrompt {
		t.Fatalf("continuation typed %q err %v", w.typed, err)
	}
	w.observation.Error = &watcher.ProviderActivityError{Code: "authentication_error", HTTPStatus: 401}
	if _, _, refused := runner.ScheduledActionRefused(Item{}, run); refused {
		t.Fatal("a sign-in failure is not a provider outage")
	}
}
