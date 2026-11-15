package brain

import (
	"errors"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// exitingWatcher is a session host on which every Session's program ends at
// once, as Brain's Claude did through an npm claude.cmd on Windows 0.4.6.
type exitingWatcher struct {
	*fakeWatcher
	exit watcher.SessionExit
}

func (w *exitingWatcher) SessionExited(id string) bool {
	_, ok := w.sessions[id]
	return ok
}

func (w *exitingWatcher) SessionExitReport(id string) (watcher.SessionExit, bool) {
	if _, ok := w.sessions[id]; !ok {
		return watcher.SessionExit{}, false
	}
	return w.exit, true
}

// A Brain whose agent ends at start shows how it ended (exit code, what it
// printed) as its problem, and the person's choice fails with it. Mewla does
// not start the same launch again every few seconds: in 0.4.6 a Windows
// user saw only "target provider could not be proven" while a new Brain
// Session started and ended every 20 to 40 seconds.
func TestABrainThatExitsAtStartShowsHowAndIsNotRespawned(t *testing.T) {
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "claude")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	code := 127
	fw := &exitingWatcher{
		fakeWatcher: &fakeWatcher{sendErr: errors.New("Session input was definitely not submitted: target provider could not be proven")},
		exit:        watcher.SessionExit{Code: &code, Ran: 400_000_000, Tail: "\x1b[8mmewla-origin 1234\nmewla sh: claude: argument 4 holds a character a .cmd program cannot be given safely (%, \", CR or LF)\n"},
	}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	}))

	_, err = service.SetHostExecutor("claude")
	if !HostExitedAtStart(err) || !strings.Contains(err.Error(), "exit code 127") || !strings.Contains(err.Error(), "argument 4 holds a character") {
		t.Fatalf("choosing claude: %v", err)
	}
	if strings.Contains(err.Error(), "mewla-origin") {
		t.Fatalf("the launch marker is not the agent's output: %v", err)
	}
	for range 3 {
		if _, err := service.ReconcileHostContinuity(); !HostExitedAtStart(err) {
			t.Fatalf("automatic start: %v", err)
		}
	}
	if len(fw.created) != 1 {
		t.Fatalf("the launch that ended was started %d times", len(fw.created))
	}
	snapshot, err := service.ProjectionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	problem := snapshot.HostProblem
	if problem == nil || problem.Code != "start_failed" || !strings.Contains(problem.Detail, "exit code 127") || !strings.Contains(problem.Detail, "argument 4 holds a character") {
		t.Fatalf("problem a client shows = %+v", problem)
	}

	// Choosing it again (after the person fixed something) tries at once.
	if _, err := service.SetHostExecutor("claude"); !HostExitedAtStart(err) || len(fw.created) != 2 {
		t.Fatalf("second choice: %v, created %d", err, len(fw.created))
	}

	// A daemon that starts again finds the ended Host and says how it ended,
	// without starting the same launch again in the same breath.
	restarted := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	}))
	if _, err := restarted.ReconcileHostContinuity(); !HostExitedAtStart(err) || len(fw.created) != 2 {
		t.Fatalf("after a restart: %v, created %d", err, len(fw.created))
	}
}

// A launch that ends the same way twice in a row is not started again by
// itself, even after the retry wait: Chat says Mewla stopped, and choosing
// the agent again tries at once.
func TestABrainThatExitsTheSameWayTwiceIsNotStartedAgainByItself(t *testing.T) {
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "claude")
	previous := hostExitRetryAfter
	hostExitRetryAfter = 0
	t.Cleanup(func() { hostExitRetryAfter = previous })
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	code := 3
	fw := &exitingWatcher{
		fakeWatcher: &fakeWatcher{sendErr: errors.New("Session input was definitely not submitted: target provider could not be proven")},
		exit:        watcher.SessionExit{Code: &code, Ran: 300_000_000, Tail: "Error: boom from fake claude: cannot start\n"},
	}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
	}))
	if _, err := service.SetHostExecutor("claude"); !HostExitedAtStart(err) {
		t.Fatalf("choosing claude: %v", err)
	}
	for range 4 {
		_, _ = service.ReconcileHostContinuity()
	}
	if len(fw.created) != 2 {
		t.Fatalf("launches = %d, want 2 (the first and one retry)", len(fw.created))
	}
	snapshot, err := service.ProjectionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if problem := snapshot.HostProblem; problem == nil || !strings.Contains(problem.Detail, "exit code 3") ||
		!strings.Contains(problem.Detail, "boom from fake claude") || !strings.Contains(problem.Detail, "Mewla stopped starting it") {
		t.Fatalf("problem = %+v", snapshot.HostProblem)
	}
	if _, err := service.SetHostExecutor("claude"); !HostExitedAtStart(err) || len(fw.created) != 3 {
		t.Fatalf("choosing it again: %v, launches %d", err, len(fw.created))
	}
}
