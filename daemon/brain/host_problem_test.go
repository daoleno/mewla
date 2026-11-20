package brain

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// hostFixture installs the given commands and sign-in states for one test.
func hostFixture(t *testing.T, installed map[string]bool, auth map[string]HostAuthState) *Service {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	previousLook := hostLookPath
	hostLookPath = func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	previousAuth := hostAuth
	hostAuth = func(id string) HostAuthState {
		if state, ok := auth[id]; ok {
			return state
		}
		return HostAuthUnknown
	}
	t.Cleanup(func() { hostLookPath, hostAuth = previousLook, previousAuth })
	return newTestService(t, store, nil, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Command: "claude", Kind: "claude"},
		"codex":  {Command: "codex", Kind: "codex"},
		"grok":   {Command: "grok --no-alt-screen", Kind: "grok"},
	}))
}

// A fresh Mac with Grok installed but never signed in, and Claude signed in
// (the report that left Brain on "Waking up"): the verified agent hosts Brain.
func TestFirstBrainHostPrefersAVerifiedSignedInAgent(t *testing.T) {
	service := hostFixture(t,
		map[string]bool{"claude": true, "grok": true},
		map[string]HostAuthState{"claude": HostAuthUnauthenticated, "grok": HostAuthUnknown})
	if got := service.hostExecutor().ID; got != "grok" {
		t.Fatalf("claude signed out, grok unknown: host = %q, want grok before a signed-out claude", got)
	}

	service = hostFixture(t,
		map[string]bool{"claude": true, "grok": true},
		map[string]HostAuthState{"grok": HostAuthAuthenticated})
	if got := service.hostExecutor().ID; got != "grok" {
		t.Fatalf("grok verified, claude unknown: host = %q, want the verified grok", got)
	}

	service = hostFixture(t,
		map[string]bool{"claude": true, "grok": true, "codex": true},
		map[string]HostAuthState{"claude": HostAuthAuthenticated, "grok": HostAuthUnknown})
	if got := service.hostExecutor().ID; got != "claude" {
		t.Fatalf("claude verified: host = %q, want claude", got)
	}

	service = hostFixture(t,
		map[string]bool{"claude": true, "grok": true},
		map[string]HostAuthState{})
	if got := service.hostExecutor().ID; got != "claude" {
		t.Fatalf("nothing verified: host = %q, want the first installed in AgentOrder", got)
	}
}

func TestHostProblemNamesWhyBrainHasNoHost(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })

	t.Run("not installed", func(t *testing.T) {
		service := hostFixture(t, map[string]bool{"claude": true}, map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@1", "grok"); err != nil {
			t.Fatal(err)
		}
		problem := service.hostProblem(service.hostExecutor())
		if problem == nil || problem.Code != "not_installed" || problem.Executor != "grok" ||
			problem.SuggestExecutor != "claude" || !strings.Contains(problem.Detail, "grok is not installed") {
			t.Fatalf("problem = %+v", problem)
		}
	})
	// Login status is no reason Brain has no host: an agent on an API key says
	// "not logged in" and runs. What is reported is that it did not start.
	t.Run("signed out by login status", func(t *testing.T) {
		service := hostFixture(t, map[string]bool{"claude": true, "codex": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated, "codex": HostAuthUnauthenticated})
		if err := service.store.SetHostSession("brain-host:@1", "codex"); err != nil {
			t.Fatal(err)
		}
		problem := service.hostProblem(service.hostExecutor())
		if problem == nil || problem.Code != "not_started" || problem.SuggestExecutor != "claude" {
			t.Fatalf("problem = %+v", problem)
		}
	})
	t.Run("did not start", func(t *testing.T) {
		service := hostFixture(t, map[string]bool{"claude": true, "grok": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@1", "grok"); err != nil {
			t.Fatal(err)
		}
		problem := service.hostProblem(service.hostExecutor())
		if problem == nil || problem.Code != "not_started" || problem.SuggestExecutor != "claude" ||
			!strings.Contains(problem.Detail, "cannot tell whether grok is signed in") {
			t.Fatalf("problem = %+v", problem)
		}
	})
	t.Run("a host still starting is no problem", func(t *testing.T) {
		previous := processStartedAt
		processStartedAt = time.Now()
		t.Cleanup(func() { processStartedAt = previous })
		service := hostFixture(t, map[string]bool{"claude": true}, map[string]HostAuthState{"claude": HostAuthUnknown})
		if problem := service.hostProblem(service.hostExecutor()); problem != nil {
			t.Fatalf("problem = %+v within the start grace", problem)
		}
	})
}

// The gate is the snapshot, not hostProblem(): a recorded host whose Session
// is gone still projects as a WorkerRef, and must still say why it is not running.
func TestSnapshotReportsAHostProblemForARecordedButUnobservedHost(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	service := hostFixture(t, map[string]bool{"claude": true},
		map[string]HostAuthState{"claude": HostAuthAuthenticated})
	if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
		t.Fatal(err)
	}
	service.watcher = &fakeWatcher{}

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for range 3 {
		snapshot, err := service.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.HostWorker == nil || snapshot.HostWorker.ID != "brain-host:@9" {
			t.Fatalf("host worker = %+v, want the recorded host", snapshot.HostWorker)
		}
		if snapshot.HostProblem == nil || snapshot.HostProblem.Code != "not_installed" ||
			snapshot.HostProblem.SuggestExecutor != "claude" {
			t.Fatalf("host problem = %+v", snapshot.HostProblem)
		}
	}
	if got := strings.Count(logged.String(), "brain host problem:"); got != 1 {
		t.Fatalf("problem logged %d times, want once: %q", got, logged.String())
	}

	// The Session appears: the problem clears, once.
	service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{
		"brain-host:@9": {ID: "brain-host:@9", Hidden: true, Command: "claude"},
	}}
	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostProblem != nil {
		t.Fatalf("host problem = %+v with a running host", snapshot.HostProblem)
	}
	if !strings.Contains(logged.String(), "brain host problem cleared") {
		t.Fatalf("recovery not logged: %q", logged.String())
	}
}

// A "Switch executor" button is a way out only when another agent is on the
// computer; with none installed it is a dead end, so the problem says which
// others are installed (signed in or not).
func TestHostProblemListsTheOtherInstalledAgents(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })

	project := func(t *testing.T, installed map[string]bool, auth map[string]HostAuthState, host string) *HostProblem {
		t.Helper()
		service := hostFixture(t, installed, auth)
		if err := service.store.SetHostSession("brain-host:@1", host); err != nil {
			t.Fatal(err)
		}
		service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{}}
		return service.projectHostProblem(WorkerRef{ID: "brain-host:@1"}, service.hostExecutor())
	}

	if problem := project(t, map[string]bool{}, nil, "claude"); problem == nil ||
		problem.Code != "not_installed" || len(problem.InstalledExecutors) != 0 {
		t.Fatalf("nothing installed: %+v", problem)
	}
	// Installed but never verified signed in still counts: the person may know better.
	problem := project(t, map[string]bool{"codex": true, "grok": true}, nil, "claude")
	if problem == nil || problem.Code != "not_installed" || problem.SuggestExecutor != "" ||
		strings.Join(problem.InstalledExecutors, ",") != "codex,grok" {
		t.Fatalf("others installed, none verified: %+v", problem)
	}
	// The host itself is never its own alternative.
	problem = project(t, map[string]bool{"claude": true, "codex": true}, map[string]HostAuthState{"claude": HostAuthUnauthenticated}, "claude")
	if problem == nil || problem.Code != "not_started" || strings.Join(problem.InstalledExecutors, ",") != "codex" {
		t.Fatalf("host signed out: %+v", problem)
	}
}

// No agent on the computer: the snapshot offers the installs, each the command
// the daemon gave (its own mewla deps -agent), and only then.
func TestSnapshotOffersAgentInstallsOnlyWhenNoAgentIsInstalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Mewla offers no agent install on Windows")
	}
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	snapshot := func(t *testing.T, installed map[string]bool, command bool) Snapshot {
		t.Helper()
		service := hostFixture(t, installed, nil)
		if err := service.store.SetHostSession("brain-host:@1", "claude"); err != nil {
			t.Fatal(err)
		}
		service.watcher = &fakeWatcher{sessions: map[string]*classifier.Worker{}}
		if command {
			service.SetAgentInstallCommand(func(id string) string { return "'/Apps/Mewla.app/mewla' deps -agent " + id })
		}
		snapshot, err := service.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	got := snapshot(t, map[string]bool{}, true)
	if got.HostProblem == nil || got.HostProblem.Code != "not_installed" || len(got.AgentInstalls) != 2 ||
		got.AgentInstalls[0] != (AgentInstall{ID: "claude", Name: "Claude Code", Command: "'/Apps/Mewla.app/mewla' deps -agent claude"}) ||
		got.AgentInstalls[1] != (AgentInstall{ID: "codex", Name: "Codex", Command: "'/Apps/Mewla.app/mewla' deps -agent codex"}) {
		t.Fatalf("nothing installed: problem %+v installs %+v", got.HostProblem, got.AgentInstalls)
	}
	// An agent installed but not the host's is a switch, not an install.
	if got := snapshot(t, map[string]bool{"codex": true}, true); got.HostProblem == nil || len(got.AgentInstalls) != 0 {
		t.Fatalf("codex installed: problem %+v installs %+v", got.HostProblem, got.AgentInstalls)
	}
	// A daemon that gave no command offers none.
	if got := snapshot(t, map[string]bool{}, false); len(got.AgentInstalls) != 0 {
		t.Fatalf("no command: %+v", got.AgentInstalls)
	}
}

// The executor sheet offers only what is installed, signed-in ones first, so
// the snapshot says which executors are installed and verified signed in.
func TestSnapshotSaysWhichExecutorsAreInstalledAndSignedIn(t *testing.T) {
	service := hostFixture(t, map[string]bool{"claude": true, "codex": true},
		map[string]HostAuthState{"claude": HostAuthAuthenticated, "codex": HostAuthUnauthenticated})
	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ExecutorState{
		"claude": {Installed: true, SignedIn: true},
		"codex":  {Installed: true},
	}
	if len(snapshot.ExecutorStates) != len(want) {
		t.Fatalf("states = %+v, want only the installed executors %+v (grok is not installed)", snapshot.ExecutorStates, want)
	}
	for id, state := range want {
		if snapshot.ExecutorStates[id] != state {
			t.Fatalf("%s = %+v, want %+v (all: %+v)", id, snapshot.ExecutorStates[id], state, snapshot.ExecutorStates)
		}
	}
}
