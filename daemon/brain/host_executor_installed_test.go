package brain

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
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

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if got := service.hostExecutor().ID; got != "claude" {
		t.Fatalf("nothing installed: host = %q, want claude, the first in the shared order", got)
	}
	installed["codex"] = true
	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("only Codex installed: host = %q, want codex", got)
	}
	if logged.Len() != 0 {
		t.Fatalf("one installed agent logged a default pick: %q", logged.String())
	}
	installed["claude"] = true
	for range 3 {
		if got := service.hostExecutor().ID; got != "claude" {
			t.Fatalf("both installed: host = %q, want claude first", got)
		}
	}
	if got := strings.Count(logged.String(), "brain host:"); got != 1 {
		t.Fatalf("default pick logged %d times, want once: %q", got, logged.String())
	}
	if !strings.Contains(logged.String(), "using claude by default (also installed: codex)") ||
		!strings.Contains(logged.String(), "mewla brain use <claude|codex>") {
		t.Fatalf("default pick log = %q", logged.String())
	}
	if err := store.SetHostSession("brain-host:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	if got := service.hostExecutor().ID; got != "codex" {
		t.Fatalf("a chosen host = %q, want it kept", got)
	}
}
