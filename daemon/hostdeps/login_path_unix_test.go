//go:build !windows

package hostdeps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An agent installed where only the person's shell looks (nvm, an npm user
// prefix, bun, volta) is found by a daemon started with a service's PATH: the
// login shell's PATH and the usual per-user Node directories are searched.
func TestLookPathFindsAgentsOnTheLoginShellPathAndNodeInstallDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/nonexistent")
	restore := SetSystemPathDirs()
	defer restore()
	shellOnly := filepath.Join(home, "tools", "agents", "bin")
	nvm := filepath.Join(home, ".nvm", "versions", "node", "v22.11.0", "bin")
	for _, dir := range []string{shellOnly, nvm} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shellOnly, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nvm, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreProbe := SetLoginShellProbe(func(string, string) ([]string, error) { return []string{shellOnly}, nil })
	defer restoreProbe()
	if got, err := LookPath("claude"); err != nil || got != filepath.Join(shellOnly, "claude") {
		t.Fatalf("claude on the login shell PATH: %q %v", got, err)
	}
	if got, err := LookPath("codex"); err != nil || got != filepath.Join(nvm, "codex") {
		t.Fatalf("codex under nvm: %q %v", got, err)
	}
	AddInstallerDirsToPath()
	if path := os.Getenv("PATH"); !strings.HasPrefix(path, "/nonexistent:") || !strings.Contains(path, shellOnly) || !strings.Contains(path, nvm) {
		t.Fatalf("PATH = %q, want the daemon's own first, then the shell's and nvm's", path)
	}
}

func TestProbeLoginShellPathReadsAnInteractiveLoginShell(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("echo noise from rc\nexport PATH=\"$HOME/from rc/bin:$PATH\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(". ~/.bashrc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	dirs, err := probeLoginShellPath(bash, home)
	if err != nil || len(dirs) == 0 || dirs[0] != filepath.Join(home, "from rc", "bin") {
		t.Fatalf("dirs = %q, %v", dirs, err)
	}
}
