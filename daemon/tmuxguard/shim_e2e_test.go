package tmuxguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The shim is the real installed script re-entering the real mewla binary. The
// "real tmux" behind it is a stub that logs its calls, so a refusal that leaked
// through would show in the log and never touch a tmux server.
func TestShimEndToEnd(t *testing.T) {
	root := t.TempDir()
	mewla := filepath.Join(root, "mewla")
	build := exec.Command("go", "build", "-o", mewla, "../cmd/mewla")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mewla: %v\n%s", err, out)
	}
	shimDir, err := Install(filepath.Join(root, "state"), mewla)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(root, "scratch")
	stubDir := filepath.Join(root, "stub")
	for _, d := range []string{scratch, stubDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(root, "calls.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + calls + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stubDir, "tmux"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(tmuxTmpdir string, args ...string) (int, string) {
		cmd := exec.Command(filepath.Join(shimDir, "tmux"), args...)
		cmd.Env = []string{
			"PATH=" + shimDir + string(os.PathListSeparator) + stubDir + ":/usr/bin:/bin",
			ScratchEnv + "=" + scratch,
			"TMUX_TMPDIR=" + tmuxTmpdir,
		}
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, string(out)
	}
	calledWith := func() string {
		data, _ := os.ReadFile(calls)
		return string(data)
	}

	missing := filepath.Join(scratch, "deleted", "tmux")
	for _, args := range [][]string{{"kill-server"}, {"ls"}, {"-S", "/tmp/tmux-1000/default", "kill-server"}} {
		code, out := run(missing, args...)
		if code != 1 || !strings.Contains(out, "refused by the Mewla tmux guard") {
			t.Fatalf("tmux %v: exit=%d out=%q, want a guard refusal with exit 1", args, code, out)
		}
	}
	if got := calledWith(); got != "" {
		t.Fatalf("a refused command reached the real tmux: %q", got)
	}

	sock := filepath.Join(scratch, "p", "s.sock")
	if code, out := run(missing, "-S", sock, "kill-server"); code != 0 {
		t.Fatalf("kill-server on a scratch -S socket: exit=%d out=%q", code, out)
	}
	if got := calledWith(); got != "-S "+sock+" kill-server\n" {
		t.Fatalf("real tmux got %q, want the original arguments untouched", got)
	}

	// A shim whose mewla binary vanished (mid-rebuild) must not fall through.
	if err := os.Remove(mewla); err != nil {
		t.Fatal(err)
	}
	code, out := run(missing, "-S", sock, "ls")
	if code != 1 || !strings.Contains(out, "refusing to run an unguarded tmux") {
		t.Fatalf("missing binary: exit=%d out=%q", code, out)
	}
	if strings.Count(calledWith(), "\n") != 1 {
		t.Fatalf("unguarded tmux ran: %q", calledWith())
	}
}
