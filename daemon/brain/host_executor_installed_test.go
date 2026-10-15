package brain

import (
	"os/exec"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func TestFirstBrainHostUsesAnInstalledExecutor(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	installed := map[string]bool{}
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { hostLookPath = previous })
	service := NewService(store, nil, work.NewExecutorConfig(map[string]work.Executor{
		"codex":  {Command: "codex", Kind: "codex"},
		"claude": {Command: "claude", Kind: "claude"},
	}))

	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("nothing installed: host = %q, want the codex default", got)
	}
	installed["claude"] = true
	if got := service.hostExecutor().ID; got != "claude" {
		t.Fatalf("only Claude Code installed: host = %q, want claude", got)
	}
	installed["codex"] = true
	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("both installed: host = %q, want codex first", got)
	}
	installed["codex"] = false
	if err := store.SetHostSession("brain-host:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("a chosen host = %q, want it kept", got)
	}
}
