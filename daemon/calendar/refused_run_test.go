package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// refusedAgentWatcher: the run's claude agent sits idle at its prompt after its
// first request was refused with HTTP 429 (2026-11-21 00:00:02, calendar-612384f7…).
type refusedAgentWatcher struct{}

func (refusedAgentWatcher) GetWorker(id string) *classifier.Worker {
	return &classifier.Worker{ID: id, Command: "claude --model claude-sonnet-5-5", State: classifier.StateDone}
}
func (refusedAgentWatcher) HasSession(string) bool { return true }
func (refusedAgentWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return watcher.ProviderActivityObservation{
		ID: "a1", Status: "completed", ProbeState: watcher.ProbeStateOK, SettledAt: time.Date(2026, 11, 21, 0, 0, 2, 0, time.UTC),
		Error:              &watcher.ProviderActivityError{Code: "rate_limit", HTTPStatus: 429, Message: "API Error: Request rejected (429)"},
		TerminalActivities: []watcher.ProviderTerminalActivity{{ID: "a1", Status: "completed"}},
	}, true, nil
}

func TestARunWhoseAgentWasRefusedAtItsFirstRequestIsHeldNotFailed(t *testing.T) {
	now := time.Date(2026, 11, 21, 0, 1, 0, 0, time.UTC)
	calendarStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calendarStore.now = func() time.Time { return now }
	due := now.Add(-time.Minute)
	item, err := calendarStore.Create(Item{
		ID: "briefing", Title: "Daily Hacker News Briefing", Kind: KindScheduledAction, DueAt: &due, Timezone: "UTC",
		Recurrence: RecurrenceDaily, ActionInstruction: "Brief me", SourceThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	workStore, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	launches := &scheduledActionLaunchRunner{newID: "mh-briefing"}
	launcher := work.NewLauncher(launches, work.NewExecutorConfig(map[string]work.Executor{"claude": {Name: "claude", Command: "claude"}}))
	runner := &WorkRunner{Store: workStore, Launcher: launcher, Watcher: refusedAgentWatcher{}, Brain: &scheduledActionThreadRegistry{known: true, host: "claude"}}
	scheduler := NewScheduler(calendarStore, runner)
	scheduler.now = func() time.Time { return now }
	if _, err := scheduler.run(context.Background(), item.ID, false); err != nil {
		t.Fatal(err)
	}
	scheduler.Tick(context.Background())
	got, err := calendarStore.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning || got.Runs[0].Status != StatusRunning {
		t.Fatalf("a run whose agent the provider refused was ended: item %s run %s %q", got.Status, got.Runs[0].Status, got.Runs[0].FailureReason)
	}
	if _, err := scheduler.RunNow(context.Background(), item.ID); err != nil {
		t.Fatalf("Run now on the refused run: %v", err)
	}
}
