package computers

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

func installFake(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func freshCaps(t *testing.T) Caps {
	t.Helper()
	// Only the directories a test fills are looked in: a real Mac has agents
	// of its own in /opt/homebrew/bin.
	t.Cleanup(hostdeps.SetSystemPathDirs())
	capsMu.Lock()
	capsAt = time.Time{}
	capsMu.Unlock()
	t.Cleanup(func() {
		capsMu.Lock()
		capsAt = time.Time{}
		capsMu.Unlock()
	})
	return DetectCaps()
}

// A computer reports the agents Mewla could launch there: the same executables
// found the same way (PATH, then the installers' directories), under Mewla's
// agent ids and in its preference order.
func TestDetectCapsReportsTheAgentsMewlaCanLaunch(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	installFake(t, bin, "codex")
	installFake(t, bin, "cursor-agent")
	installFake(t, filepath.Join(home, ".local", "bin"), "claude")
	installFake(t, filepath.Join(home, ".opencode", "bin"), "opencode")

	got := freshCaps(t).Agents
	want := []string{"claude", "codex", "agent", "opencode"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
}

func TestDetectCapsDoesNotReportAnAgentUnderItsExecutableName(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin)
	// An unrelated "agent" program must not make the computer offer Cursor.
	installFake(t, bin, "agent")

	if got := freshCaps(t).Agents; len(got) != 0 {
		t.Fatalf("agents = %v, want none", got)
	}
}
