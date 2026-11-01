package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func workerSession(id, command string) map[string]*classifier.Worker {
	return map[string]*classifier.Worker{id: {ID: id, Hidden: true, Command: command}}
}

func healFixture(t *testing.T, installed map[string]bool, auth map[string]HostAuthState) *Service {
	t.Helper()
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	service := hostFixture(t, installed, auth)
	service.watcher = &fakeWatcher{}
	return service
}

func hostExecutorOnDisk(t *testing.T, service *Service) string {
	t.Helper()
	session, err := service.store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	return session.ExecutorID
}

// A default pick that turns out unusable moves itself to an agent verified
// signed in, so onboarding never leaves a person on a Brain that cannot start.
func TestDefaultPickedHostMovesToAVerifiedAgent(t *testing.T) {
	t.Run("not installed moves at once", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if !service.healImplicitHost() {
			t.Fatal("a missing agent did not move")
		}
		if got := hostExecutorOnDisk(t, service); got != "claude" {
			t.Fatalf("executor = %q, want claude", got)
		}
		if service.store.HostExecutorExplicit() != "" {
			t.Fatal("a move by Mewla was recorded as a person's choice")
		}
	})
	t.Run("signed out moves at once", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "codex": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated, "codex": HostAuthUnauthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "codex"); err != nil {
			t.Fatal(err)
		}
		if !service.healImplicitHost() || hostExecutorOnDisk(t, service) != "claude" {
			t.Fatalf("executor = %q, want claude", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("running host at a sign-in prompt moves", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "codex": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated, "codex": HostAuthUnauthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "codex"); err != nil {
			t.Fatal(err)
		}
		service.watcher = &fakeWatcher{sessions: workerSession("brain-host:@9", "codex")}
		if !service.healImplicitHost() || hostExecutorOnDisk(t, service) != "claude" {
			t.Fatalf("executor = %q, want claude", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("a host that did not start moves after the grace", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "grok": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if service.healImplicitHost() {
			t.Fatal("moved before the host had been down for the grace")
		}
		service.hostProblemMu.Lock()
		service.hostProblemSince = time.Now().Add(-hostHealGrace - time.Second)
		service.hostProblemMu.Unlock()
		if !service.healImplicitHost() || hostExecutorOnDisk(t, service) != "claude" {
			t.Fatalf("executor = %q, want claude", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("an unsupported version moves at once", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "grok": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		hostUnsupported = func(id string) string {
			if id == "grok" {
				return "Grok 0.0.1 found; Mewla supports a newer version."
			}
			return ""
		}
		t.Cleanup(func() { SetHostUnsupportedProbe(nil) })
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if !service.healImplicitHost() || hostExecutorOnDisk(t, service) != "claude" {
			t.Fatalf("executor = %q, want claude", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("an agent open at a login wall moves after the grace", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "grok": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		service.watcher = &fakeWatcher{sessions: workerSession("brain-host:@9", "grok")}
		service.hostProblemMu.Lock()
		service.hostNotReadySince = time.Now().Add(-2 * hostNotReadyGrace)
		service.hostProblemMu.Unlock()
		if service.healImplicitHost() {
			t.Fatal("moved on the first sight of the problem, before the grace")
		}
		service.hostProblemMu.Lock()
		service.hostProblemSince = time.Now().Add(-hostHealGrace - time.Second)
		service.hostProblemMu.Unlock()
		if !service.healImplicitHost() || hostExecutorOnDisk(t, service) != "claude" {
			t.Fatalf("executor = %q, want claude", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("moves at most once per cooldown", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true, "codex": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if !service.healImplicitHost() {
			t.Fatal("did not move")
		}
		if err := service.store.setImplicitHostExecutorID("grok"); err != nil {
			t.Fatal(err)
		}
		if service.healImplicitHost() {
			t.Fatal("moved again inside the cooldown")
		}
	})
}

func TestChosenHostIsNeverMoved(t *testing.T) {
	t.Run("mewla brain use stays", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if err := service.store.SetHostExecutorID("grok"); err != nil {
			t.Fatal(err)
		}
		if service.store.HostExecutorExplicit() != "grok" {
			t.Fatal("the choice was not recorded")
		}
		if service.healImplicitHost() || hostExecutorOnDisk(t, service) != "grok" {
			t.Fatalf("a chosen agent moved to %q", hostExecutorOnDisk(t, service))
		}
	})
	t.Run("first-run choice stays", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		if err := SaveHostExecutorID(service.store.Root, "grok"); err != nil {
			t.Fatal(err)
		}
		if service.healImplicitHost() {
			t.Fatal("a first-run choice moved")
		}
	})
	t.Run("environment override stays", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true},
			map[string]HostAuthState{"claude": HostAuthAuthenticated})
		t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "grok")
		if service.healImplicitHost() {
			t.Fatal("an environment-locked agent moved")
		}
	})
	t.Run("nothing verified to move to stays", func(t *testing.T) {
		service := healFixture(t, map[string]bool{"claude": true},
			map[string]HostAuthState{"claude": HostAuthUnknown})
		if err := service.store.SetHostSession("brain-host:@9", "grok"); err != nil {
			t.Fatal(err)
		}
		if service.healImplicitHost() || hostExecutorOnDisk(t, service) != "grok" {
			t.Fatalf("moved to %q with nothing verified", hostExecutorOnDisk(t, service))
		}
	})
}
