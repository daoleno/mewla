package providerpaths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClaudeConfigDirHonoursOverride(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := ClaudeConfigDir("/home/u"); got != filepath.Join("/home/u", ".claude") {
		t.Fatalf("default = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/srv/claude")
	if got := ClaudeConfigDir("/home/u"); got != "/srv/claude" {
		t.Fatalf("override = %q", got)
	}
}

func TestPiAgentDirExpandsHome(t *testing.T) {
	for override, want := range map[string]string{
		"":          filepath.Join("/home/u", ".pi", "agent"),
		"~":         "/home/u",
		"~/pi-data": filepath.Join("/home/u", "pi-data"),
		"/srv/pi":   "/srv/pi",
	} {
		t.Setenv("PI_CODING_AGENT_DIR", override)
		if got := PiAgentDir("/home/u"); got != want {
			t.Fatalf("PI_CODING_AGENT_DIR=%q: got %q, want %q", override, got, want)
		}
	}
}

func TestOpenCodeDBOverrideAndFallback(t *testing.T) {
	t.Setenv("MEWLA_OPENCODE_DB", "/srv/opencode.db")
	if got := OpenCodeDB("/home/u"); got != "/srv/opencode.db" {
		t.Fatalf("override = %q", got)
	}

	// Without opencode on PATH, the platform default is used only once the
	// file exists.
	t.Setenv("MEWLA_OPENCODE_DB", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	resetOpenCodeDB(t)
	home := t.TempDir()
	if got := OpenCodeDB(home); got != "" {
		t.Fatalf("missing database resolved to %q", got)
	}
	want := openCodeDefaultDBPath(home)
	if runtime.GOOS == "linux" && want != filepath.Join(home, ".local", "share", "opencode", "opencode.db") {
		t.Fatalf("linux default = %q", want)
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := OpenCodeDB(home); got != want {
		t.Fatalf("fallback = %q, want %q", got, want)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	if got := openCodeDefaultDBPath(home); got != filepath.Join(xdg, "opencode", "opencode.db") {
		t.Fatalf("XDG default = %q", got)
	}
}

func resetOpenCodeDB(t *testing.T) {
	t.Helper()
	openCodeDBMu.Lock()
	openCodeDBResolved = ""
	openCodeDBMu.Unlock()
	t.Cleanup(func() {
		openCodeDBMu.Lock()
		openCodeDBResolved = ""
		openCodeDBMu.Unlock()
	})
}
