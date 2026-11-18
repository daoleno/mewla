package trigger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellArgvChoosesTheShell(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	found := func(name string) (string, error) {
		if name == "pwsh" {
			return `C:\Program Files\PowerShell\7\pwsh.exe`, nil
		}
		return "", errors.New("not found")
	}
	if got := ShellArgv("linux", found, "gh pr list"); strings.Join(got, "|") != "/bin/sh|-c|gh pr list" {
		t.Fatalf("linux %q", got)
	}
	if got := ShellArgv("darwin", none, "x"); got[0] != "/bin/sh" {
		t.Fatalf("darwin %q", got)
	}
	if got := ShellArgv("windows", found, "gh pr list"); strings.Join(got, "|") != `C:\Program Files\PowerShell\7\pwsh.exe|-NoProfile|-NonInteractive|-Command|gh pr list` {
		t.Fatalf("windows with pwsh %q", got)
	}
	if got := ShellArgv("windows", none, "gh pr list"); strings.Join(got, "|") != "powershell.exe|-NoProfile|-NonInteractive|-Command|gh pr list" {
		t.Fatalf("windows without pwsh %q", got)
	}
}

func TestCheckFindsToolsOnlyInLocalBinWithABarePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mewla-trigger-probe"), []byte("#!/bin/sh\necho found-in-local-bin\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	result := RunCheck(context.Background(), Trigger{ID: "id", Check: "mewla-trigger-probe; echo \"$MEWLA_TRIGGER_ID\"", TimeoutSeconds: 10})
	if result.Failed() || result.Stdout != "found-in-local-bin\nid\n" {
		t.Fatalf("result %+v", result)
	}
	env := CheckEnv([]string{"PATH=/usr/bin:/bin", "HOME=" + home}, "id")
	var path string
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	if !strings.HasPrefix(path, "/usr/bin:/bin:") || !strings.Contains(path, bin) {
		t.Fatalf("PATH %q", path)
	}
}

func TestRunCheckFailuresAndLimits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	dir := t.TempDir()
	result := RunCheck(context.Background(), Trigger{Check: "echo oops >&2; exit 3", Cwd: dir, TimeoutSeconds: 10})
	if result.Exit != 3 || !result.Failed() || !strings.Contains(result.Failure(), "exit status 3\noops") {
		t.Fatalf("exit %+v", result)
	}
	result = RunCheck(context.Background(), Trigger{Check: "head -c 70000 /dev/zero | tr '\\0' x", Cwd: dir, TimeoutSeconds: 10})
	if result.Failed() || len(result.Stdout) != MaxOutputBytes || !result.Truncated {
		t.Fatalf("cap: %d bytes truncated=%v", len(result.Stdout), result.Truncated)
	}
	result = RunCheck(context.Background(), Trigger{Check: "pwd", Cwd: dir, TimeoutSeconds: 10})
	if strings.TrimSpace(result.Stdout) != dir {
		t.Fatalf("cwd %q", result.Stdout)
	}
	// A timeout kills the whole group: the background sleep's pipe closes too.
	started := time.Now()
	result = RunCheck(context.Background(), Trigger{Check: "sleep 30 & sleep 30; echo late", Cwd: dir, TimeoutSeconds: 1})
	if !result.Failed() || !strings.Contains(result.Err, "timed out after 1s") || time.Since(started) > 10*time.Second {
		t.Fatalf("timeout %+v after %s", result, time.Since(started))
	}
	if !strings.Contains(RunCheck(context.Background(), Trigger{Check: "cat", Cwd: dir, TimeoutSeconds: 5}).Stdout, "") {
		t.Fatal("stdin is not empty")
	}
}
