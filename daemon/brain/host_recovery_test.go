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

// An agent may sit at its own login wall: the pane is open, input never
// becomes ready. The computer says so after a grace, and keeps retrying, so the
// person signing in there is enough.
func TestAnOpenButNeverReadyHostIsReportedAndRetried(t *testing.T) {
	service := hostFixture(t, map[string]bool{"claude": true, "grok": true})
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
		snapshot.HostProblem.Executor != "grok" || snapshot.HostProblem.SuggestExecutor != "" {
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
	service.noteHostActivation(errors.New("session host unavailable"))
	if service.hostActivationStuck() {
		t.Fatal("unrelated error marked the host not ready")
	}
}

// Every problem the computer states ends with what to do about it.
func TestHostProblemDetailsNameTheNextStep(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	missing := hostFixture(t, map[string]bool{})
	problem := missing.hostProblem(missing.hostExecutor())
	if problem == nil || problem.Code != "not_installed" || !strings.Contains(problem.Detail, "mewla deps") {
		t.Fatalf("not installed: %+v", problem)
	}
	// A host that opened and is not ready says only that, never a guess about
	// sign-in.
	signedOut := hostFixture(t, map[string]bool{"claude": true})
	signedOut.noteHostActivation(watcher.ErrWorkerInputNotReady)
	pastNotReadyGrace(signedOut)
	problem = signedOut.hostNotReadyProblem(signedOut.hostExecutor())
	if problem == nil || problem.Code != "not_ready" || strings.Contains(strings.ToLower(problem.Detail), "sign") {
		t.Fatalf("not ready: %+v", problem)
	}
	// The agent's own screen is quoted: its real words, not Mewla's guess.
	if err := signedOut.store.SetHostSession("brain-host:@3", "claude"); err != nil {
		t.Fatal(err)
	}
	signedOut.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{"brain-host:@3": {
		ID: "brain-host:@3", Hidden: true, Command: "claude",
		LastLines: []string{"Welcome to Claude Code", "", "Select login method:", "────────", "› 1. Claude account with subscription", "  2. Anthropic Console account", ""},
	}}}
	problem = signedOut.hostNotReadyProblem(signedOut.hostExecutor())
	if problem == nil || !strings.HasSuffix(problem.Detail, " It shows: Welcome to Claude Code / Select login method: / › 1. Claude account with subscription / 2. Anthropic Console account") {
		t.Fatalf("screen not quoted: %+v", problem)
	}
}

// An agent that is installed but of a version Mewla does not support (OpenCode
// 1.x) gets no pane, says why with doctor's own words, and starts once fixed.
func TestAnUnsupportedAgentIsNotLaunchedAndSaysWhy(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	service := hostFixture(t, map[string]bool{"claude": true})
	fw := &fakeWatcher{}
	service.watcher = fw
	reason := "OpenCode 1.1.25 found; Mewla supports OpenCode 2 only. Upgrade: curl -fsSL https://opencode.ai/v2/install | bash"
	hostUnsupported = func(id string) string {
		if id == "claude" {
			return reason
		}
		return ""
	}
	t.Cleanup(func() { SetHostUnsupportedProbe(nil) })

	if _, err := service.EnsureHostSnapshot(); !errors.Is(err, ErrHostNotRunnable) {
		t.Fatalf("error = %v, want ErrHostNotRunnable", err)
	}
	if len(fw.created) != 0 {
		t.Fatalf("launched an unsupported agent: %#v", fw.created)
	}
	problem := service.hostProblem(service.hostExecutor())
	if problem == nil || problem.Code != "unsupported" || !strings.Contains(problem.Detail, "supports OpenCode 2 only") || !strings.Contains(problem.Detail, "starts by itself") {
		t.Fatalf("problem = %+v", problem)
	}
	if service.HostRecoveryDue() {
		t.Fatal("recovery due while the version is still unsupported")
	}
	hostUnsupported = func(string) string { return "" }
	if !service.HostRecoveryDue() {
		t.Fatal("recovery not due after the upgrade")
	}
}

// Every agent is unready for a moment while it starts, so nothing is said
// within the grace; a pane still unready after it says so, with what the agent
// shows. A pane that becomes ready never counts as stuck.
func TestAnUnreadyPaneSaysSoAfterTheGrace(t *testing.T) {
	service := hostFixture(t, map[string]bool{"claude": true})
	if err := service.store.SetHostSession("brain-host:@1", "claude"); err != nil {
		t.Fatal(err)
	}
	service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{
		"brain-host:@1": {ID: "brain-host:@1", Hidden: true, Command: "claude"},
	}}
	notReady := fmt.Errorf("deliver Brain Host activation: %w", fmt.Errorf("%w for %q", watcher.ErrWorkerInputNotReady, "claude"))

	if snapshot, err := service.Snapshot(); err != nil || snapshot.HostProblem != nil {
		t.Fatalf("ready pane: %v %+v", err, snapshot.HostProblem)
	}
	service.noteHostActivation(notReady)
	if snapshot, _ := service.Snapshot(); snapshot.HostProblem != nil {
		t.Fatalf("starting pane reported: %+v", snapshot.HostProblem)
	}
	pastNotReadyGrace(service)
	snapshot, _ := service.Snapshot()
	if snapshot.HostProblem == nil || snapshot.HostProblem.Code != "not_ready" {
		t.Fatalf("host problem = %+v", snapshot.HostProblem)
	}
	service.noteHostActivation(nil)
	if snapshot, _ = service.Snapshot(); snapshot.HostProblem != nil {
		t.Fatalf("not cleared: %+v", snapshot.HostProblem)
	}
}

// pastNotReadyGrace makes the Host's unready pane older than the grace.
func pastNotReadyGrace(service *Service) {
	service.hostProblemMu.Lock()
	service.hostNotReadySince = time.Now().Add(-hostNotReadyGrace - time.Second)
	service.hostProblemMu.Unlock()
}

// The owner's photo (Mewla for Mac, first install, Codex 0.172 as Brain):
// "Brain's codex session is open but not ready" while Codex was working on
// the activation and Brain had answered. Whatever path delivers the
// activation, a Host that took it is not "open but not ready".
func TestHostThatTookItsActivationIsNeverReportedNotReady(t *testing.T) {
	const id = "brain-host:@1"
	notReady := fmt.Errorf("deliver Brain Host activation: %w", fmt.Errorf("%w for %q", watcher.ErrWorkerInputNotReady, "codex"))
	stuck := func(t *testing.T) *Service {
		t.Helper()
		service := hostFixture(t, map[string]bool{"claude": true, "codex": true})
		if err := service.store.SetHostSession(id, "codex"); err != nil {
			t.Fatal(err)
		}
		service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{
			id: {ID: id, Hidden: true, Command: "codex", ProcessID: 42, StartedAt: time.Now()},
		}}
		service.noteHostActivation(notReady)
		pastNotReadyGrace(service)
		if snapshot, _ := service.Snapshot(); snapshot.HostProblem == nil || snapshot.HostProblem.Code != "not_ready" {
			t.Fatalf("precondition: host problem = %+v", snapshot.HostProblem)
		}
		return service
	}
	t.Run("activation accepted outside a reconcile", func(t *testing.T) {
		// A new chat, a host choice or a restart's adoption delivers the
		// activation through ensureHostActivation; only a reconcile used to
		// end the not-ready state.
		service := stuck(t)
		if err := service.ensureHostActivation(id, "codex", service.hostExecutor(), false, false); err != nil {
			t.Fatal(err)
		}
		if snapshot, _ := service.Snapshot(); snapshot.HostProblem != nil {
			t.Fatalf("host that took its activation still reported: %+v", snapshot.HostProblem)
		}
		if service.HostRecoveryDue() {
			t.Fatal("an activated host is still retried as stuck")
		}
	})
	t.Run("activation delivery ambiguous", func(t *testing.T) {
		// The bytes reached the composer, so it was ready; every later
		// reconcile says "refusing replay", never success.
		service := stuck(t)
		service.noteHostActivation(fmt.Errorf("%w: receipt %q; refusing replay", ErrHostActivationAmbiguous, "r"))
		if snapshot, _ := service.Snapshot(); snapshot.HostProblem != nil {
			t.Fatalf("host whose activation reached it still reported: %+v", snapshot.HostProblem)
		}
	})
	t.Run("activation recorded while reconciles keep failing", func(t *testing.T) {
		service := stuck(t)
		if err := service.store.MarkHostActivation(HostActivation{SessionID: id, HostGeneration: "g", ContractDigest: brainHostContractDigest()}); err != nil {
			t.Fatal(err)
		}
		service.noteHostActivation(notReady)
		if snapshot, _ := service.Snapshot(); snapshot.HostProblem != nil {
			t.Fatalf("host with a recorded activation still reported: %+v", snapshot.HostProblem)
		}
	})
}
