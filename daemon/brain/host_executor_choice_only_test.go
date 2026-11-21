package brain

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func choiceOnlyExecutors() *work.ExecutorConfig {
	return work.NewExecutorConfig(map[string]work.Executor{
		"agy":    {Name: "agy", Command: "agy", Kind: "antigravity"},
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	})
}

// Mewla never picks agy as Brain's host by itself, even when it is the only
// agent installed: that person gets the "install an agent that can host Brain"
// answer, not a Brain that can drop a Worker's result.
func TestMewlaNeverPicksAntigravityAsBrainsHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	installed := map[string]bool{"agy": true}
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { hostLookPath = previous })
	service := newTestService(t, store, nil, choiceOnlyExecutors())

	if got := service.hostExecutor().ID; got == "agy" {
		t.Fatalf("agy alone was picked as Brain's host")
	}
	if got := service.installedAlternativeHosts(""); len(got) != 0 {
		t.Fatalf("agy alone is offered as an alternative host: %v", got)
	}
	installed["codex"] = true
	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("agy and codex installed: host = %q, want codex", got)
	}
	if got := service.installedAlternativeHosts("codex"); len(got) != 0 {
		t.Fatalf("alternatives to codex = %v, want none (agy is not offered)", got)
	}
}

// `mewla brain use agy` is the person's knowing choice and works.
func TestExplicitUseOfAntigravityStartsBrainOnIt(t *testing.T) {
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := newTestService(t, store, fw, choiceOnlyExecutors())
	if _, err := service.SetHostExecutor("agy"); err != nil {
		t.Fatalf("brain use agy: %v", err)
	}
	if got := service.hostExecutor().ID; got != "agy" {
		t.Fatalf("host = %q, want agy", got)
	}
	if len(fw.created) == 0 || !strings.Contains(fw.created[0].opts.Command, "agy") || !strings.Contains(fw.created[0].opts.Command, "--dangerously-skip-permissions") {
		t.Fatalf("created = %+v", fw.created)
	}
}
