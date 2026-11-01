package brain

import (
	"os/exec"
	"strings"
	"testing"
	"time"

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
	return NewService(store, nil, work.NewExecutorConfig(map[string]work.Executor{
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
	t.Run("signed out", func(t *testing.T) {
		service := hostFixture(t, map[string]bool{"claude": true, "codex": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated, "codex": HostAuthUnauthenticated})
		if err := service.store.SetHostSession("brain-host:@1", "codex"); err != nil {
			t.Fatal(err)
		}
		problem := service.hostProblem(service.hostExecutor())
		if problem == nil || problem.Code != "not_signed_in" || problem.SuggestExecutor != "claude" {
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
		processStartedAt = time.Now()
		service := hostFixture(t, map[string]bool{"claude": true}, map[string]HostAuthState{"claude": HostAuthUnknown})
		if problem := service.hostProblem(service.hostExecutor()); problem != nil {
			t.Fatalf("problem = %+v within the start grace", problem)
		}
	})
}
