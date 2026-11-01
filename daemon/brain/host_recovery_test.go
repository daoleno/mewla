package brain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// An agent whose sign-in Mewla cannot probe may sit at a login wall: the pane
// is open, input never becomes ready. The computer says so after a grace, and
// keeps retrying, so the person signing in is enough.
func TestAnOpenButNeverReadyHostIsReportedAndRetried(t *testing.T) {
	service := hostFixture(t, map[string]bool{"claude": true, "grok": true},
		map[string]HostAuthState{"claude": HostAuthAuthenticated})
	if err := service.store.SetHostSession("brain-host:@1", "grok"); err != nil {
		t.Fatal(err)
	}
	service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{
		"brain-host:@1": {ID: "brain-host:@1", Hidden: true, Command: "grok"},
	}}
	notReady := fmt.Errorf("deliver Brain Host activation: %w", fmt.Errorf("%w for %q", watcher.ErrWorkerInputNotReady, "grok"))

	snapshot, err := service.Snapshot()
	if err != nil || snapshot.HostProblem != nil {
		t.Fatalf("a running host before any failure: %v %+v", err, snapshot.HostProblem)
	}
	if service.HostRecoveryDue() {
		t.Fatal("recovery due for a host that is starting normally")
	}

	service.noteHostActivation(notReady)
	if snapshot, _ = service.Snapshot(); snapshot.HostProblem != nil {
		t.Fatalf("reported inside the grace: %+v", snapshot.HostProblem)
	}
	service.hostProblemMu.Lock()
	service.hostNotReadySince = time.Now().Add(-2 * hostNotReadyGrace)
	service.hostProblemMu.Unlock()
	snapshot, _ = service.Snapshot()
	if snapshot.HostProblem == nil || snapshot.HostProblem.Code != "not_ready" ||
		snapshot.HostProblem.Executor != "grok" || snapshot.HostProblem.SuggestExecutor != "claude" {
		t.Fatalf("host problem = %+v", snapshot.HostProblem)
	}
	if !service.HostRecoveryDue() {
		t.Fatal("a stuck activation is not retried")
	}

	// The person signed in: activation succeeds, the problem clears.
	service.noteHostActivation(nil)
	if snapshot, _ = service.Snapshot(); snapshot.HostProblem != nil {
		t.Fatalf("problem not cleared: %+v", snapshot.HostProblem)
	}
	// Another kind of failure never reads as "not ready".
	service.noteHostActivation(errors.New("tmux unavailable"))
	if service.hostActivationStuck() {
		t.Fatal("unrelated error marked the host not ready")
	}
}

// Every problem the computer states ends with what to do about it.
func TestHostProblemDetailsNameTheNextStep(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	missing := hostFixture(t, map[string]bool{}, nil)
	problem := missing.hostProblem(missing.hostExecutor())
	if problem == nil || problem.Code != "not_installed" || !strings.Contains(problem.Detail, "mewla deps") {
		t.Fatalf("not installed: %+v", problem)
	}
	signedOut := hostFixture(t, map[string]bool{"claude": true}, map[string]HostAuthState{"claude": HostAuthUnauthenticated})
	problem = signedOut.hostProblem(signedOut.hostExecutor())
	if problem == nil || problem.Code != "not_signed_in" || !strings.Contains(problem.Detail, "claude auth login") {
		t.Fatalf("not signed in: %+v", problem)
	}
}
