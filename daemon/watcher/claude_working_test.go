package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A Claude Session the person started (no Brain turn) is running while its
// footer says it works, since the poll that first saw it working, and idle again
// when the footer does not. A prompt ends the run: the wait for the person is not
// counted, and the answer starts a new run; a Brain-delegated Worker keeps its ledger turn's status.
func TestPollClaudeSessionRunsWhileItsFooterSaysItWorks(t *testing.T) {
	const id = "claude-session:@1"
	w := New(time.Second)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	start := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	w.pollNow = fakePollClock([]time.Time{start, start.Add(2 * time.Second), start.Add(4 * time.Second), start.Add(6 * time.Second), start.Add(8 * time.Second), start.Add(10 * time.Second)})
	pane := func(footer string) string {
		return "Claude Code v2.1.322\n\n❯ Run curl\n\n● Running curl -sI https://example.org\n\n────────\n❯ \n────────\n  " + footer + "\n"
	}
	windows := []sessionPane{{target: id, name: "proj", cwd: "/repo/proj", command: "claude", panePID: 555}}
	content := map[string]string{id: pane("⏸ manual mode on · esc to interrupt")}
	restore := installFakePollSeams(w, windows, content, map[int]processInfo{555: fakeProcess(555, start.Add(-time.Hour))})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	worker := workerByID(w.Workers(), id)
	if worker == nil || worker.State != classifier.StateRunning || worker.RunningSince == nil || !worker.RunningSince.Equal(start) {
		t.Fatalf("a working Session = %+v", worker)
	}
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateRunning || !worker.RunningSince.Equal(start) {
		t.Fatalf("the run's start moved on the next poll: %+v", worker)
	}
	// It stops at a permission prompt: the wait is the person's, not the agent's.
	content[id] = "Claude Code v2.1.322\n Bash command\n curl -sI https://example.org\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend\n"
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateBlocked || worker.RunningSince != nil {
		t.Fatalf("a Session at a prompt = %+v", worker)
	}
	content[id] = pane("⏸ manual mode on · esc to interrupt")
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateRunning || worker.RunningSince == nil || !worker.RunningSince.Equal(start.Add(6*time.Second)) {
		t.Fatalf("the answer did not start a new run: %+v", worker)
	}
	content[id] = pane("⏸ manual mode on · ? for shortcuts")
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateUnknown || worker.RunningSince != nil {
		t.Fatalf("an idle Session = %+v", worker)
	}
}

// A Brain-delegated Worker's run starts when its turn was accepted; like a
// Session the person started, a prompt ends the run and the answer starts a new
// one, so its elapsed time never counts the wait for the person.
func TestPollDelegatedWorkerRunsSinceItsTurnWasAccepted(t *testing.T) {
	const id = "mewla-worker-worker:@1"
	accepted := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	w := New(time.Second)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	w.pollNow = fakePollClock([]time.Time{accepted.Add(time.Second), accepted.Add(3 * time.Second), accepted.Add(5 * time.Second), accepted.Add(7 * time.Second)})
	ledger := newFakeTurnLedger()
	ledger.seed(id, TurnSnapshot{SessionID: id, TurnID: id + ":turn:1", Status: TurnRunning, AcceptedAt: accepted, ProcessIdentity: "recorded-proc"})
	w.turnLedger = ledger
	working := "Claude Code v2.1.322\n\n❯ Run curl\n\n● Running curl -sI https://example.org\n\n────────\n❯ \n────────\n  ⏸ manual mode on · esc to interrupt\n"
	windows := []sessionPane{{target: id, name: "worker", cwd: "/repo/mewla", command: "claude", panePID: 333, delegated: true}}
	content := map[string]string{id: working}
	restore := installFakePollSeams(w, windows, content, map[int]processInfo{333: fakeProcess(333, accepted.Add(-time.Hour))})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	if worker := workerByID(w.Workers(), id); worker == nil || worker.State != classifier.StateRunning || worker.RunningSince == nil || !worker.RunningSince.Equal(accepted) {
		t.Fatalf("a delegated Worker at work = %+v", worker)
	}
	content[id] = "Claude Code v2.1.322\n Bash command\n curl -sI https://example.org\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend\n"
	w.poll()
	drainWatcherEvents(w)
	if worker := workerByID(w.Workers(), id); worker.State != classifier.StateBlocked || worker.RunningSince != nil {
		t.Fatalf("a delegated Worker at a prompt = %+v", worker)
	}
	content[id] = working
	w.poll()
	drainWatcherEvents(w)
	if worker := workerByID(w.Workers(), id); worker.State != classifier.StateRunning || worker.RunningSince == nil || !worker.RunningSince.Equal(accepted.Add(5*time.Second)) {
		t.Fatalf("the answer did not start a new run: %+v", worker)
	}
}
