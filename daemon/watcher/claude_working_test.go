package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A Claude Session the person started (no Brain turn) is running while its
// footer says it works, since the poll that first saw it, and idle again when
// the footer does not; a Brain-delegated Worker keeps its ledger turn's status.
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
	// It stops at a permission prompt and goes on once it is answered: one run.
	content[id] = "Claude Code v2.1.322\n Bash command\n curl -sI https://example.org\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend\n"
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateBlocked || worker.RunningSince == nil || !worker.RunningSince.Equal(start) {
		t.Fatalf("a Session at a prompt = %+v", worker)
	}
	content[id] = pane("⏸ manual mode on · esc to interrupt")
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateRunning || worker.RunningSince == nil || !worker.RunningSince.Equal(start) {
		t.Fatalf("the run restarted after its prompt: %+v", worker)
	}
	content[id] = pane("⏸ manual mode on · ? for shortcuts")
	w.poll()
	drainWatcherEvents(w)
	if worker = workerByID(w.Workers(), id); worker.State != classifier.StateUnknown || worker.RunningSince != nil {
		t.Fatalf("an idle Session = %+v", worker)
	}
}
