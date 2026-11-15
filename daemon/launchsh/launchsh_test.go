package launchsh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
)

type started struct {
	argv []string
	env  map[string]string
	dir  string
}

func recorder(code int, calls *[]started) Starter {
	return func(_ context.Context, argv, env []string, dir string, _ io.Reader, _, _ io.Writer) (int, error) {
		vars := map[string]string{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			vars[k] = v
		}
		*calls = append(*calls, started{argv: argv, env: vars, dir: dir})
		return code, nil
	}
}

// A Worker's launch as the watcher builds it: origin marker, then the login
// shell wrapper around the progress set-up and the agent.
const workerLaunch = `printf '\033[8m%s\033[0m\n' 'mewla-origin 0123abcd'; exec '/bin/sh' -i -l -c 'MEWLA_WORKER_ID="${MEWLA_SESSION_ID:-$TMUX_PANE}"; if [ -z "${MEWLA_WORKER_PROGRESS_CMD:-}" ]; then MEWLA_WORKER_PROGRESS_CMD='\''C:\Users\ana b\mewla.exe'\''; fi; export MEWLA_WORKER_ID MEWLA_WORKER_PROGRESS_CMD; unset TMUX; if [ -n "${MEWLA_TMUX_GUARD_DIR:-}" ]; then PATH="$MEWLA_TMUX_GUARD_DIR:$PATH"; export PATH; fi; claude --session-id 501f --settings '\''{"env":{"A":"b c"}}'\'' --allowedTools mcp__x'`

func TestRunsAWorkerLaunchWithoutASystemShell(t *testing.T) {
	var calls []started
	var out bytes.Buffer
	code, err := Run(context.Background(), workerLaunch, Options{
		Env:    []string{`PATH=C:\Windows\system32`, "TMUX=x", "MEWLA_SESSION_ID=mh-0001"},
		Dir:    t.TempDir(),
		Stdout: &out, Stderr: &out,
		Start: recorder(0, &calls),
	})
	if err != nil || code != 0 {
		t.Fatalf("Run = %d, %v; output %q", code, err, out.String())
	}
	if out.String() != "\x1b[8mmewla-origin 0123abcd\x1b[0m\n" {
		t.Fatalf("origin marker = %q", out.String())
	}
	if len(calls) != 1 {
		t.Fatalf("started %d programs: %+v", len(calls), calls)
	}
	got := calls[0]
	want := []string{"claude", "--session-id", "501f", "--settings", `{"env":{"A":"b c"}}`, "--allowedTools", "mcp__x"}
	if strings.Join(got.argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q", got.argv)
	}
	if got.env["MEWLA_WORKER_ID"] != "mh-0001" || got.env["MEWLA_WORKER_PROGRESS_CMD"] != `C:\Users\ana b\mewla.exe` {
		t.Fatalf("progress environment = %q / %q", got.env["MEWLA_WORKER_ID"], got.env["MEWLA_WORKER_PROGRESS_CMD"])
	}
	if _, ok := got.env["TMUX"]; ok {
		t.Fatal("TMUX was not unset")
	}
	if got.env["PATH"] != `C:\Windows\system32` {
		t.Fatalf("PATH = %q", got.env["PATH"])
	}
}

func TestExitStatusIsTheProgramsAndShellsNestOnlyWithC(t *testing.T) {
	var calls []started
	code, err := Run(context.Background(), `exec bash -l -c 'exec agent run'`, Options{Stdout: io.Discard, Stderr: io.Discard, Start: recorder(7, &calls)})
	if err != nil || code != 7 || len(calls) != 1 || calls[0].argv[0] != "agent" {
		t.Fatalf("code %d, err %v, calls %+v", code, err, calls)
	}
	calls = nil
	code, _ = Run(context.Background(), `zsh -i`, Options{Stdout: io.Discard, Stderr: io.Discard, Start: recorder(0, &calls)})
	if len(calls) != 1 || calls[0].argv[0] != "zsh" || code != 0 {
		t.Fatalf("an interactive shell without -c is a program: %+v", calls)
	}
	if code, _ := Run(context.Background(), `exit 3`, Options{Stdout: io.Discard, Stderr: io.Discard, Start: recorder(0, &calls)}); code != 3 {
		t.Fatalf("exit 3 = %d", code)
	}
}

// A program that cannot start is named once in what the Session prints (the
// line Brain's start failure quotes in Chat): "mewla sh: grok: executable file
// not found", not "grok: grok: ...".
func TestAProgramThatCannotStartIsNamedOnce(t *testing.T) {
	notFound := func(_ context.Context, argv, _ []string, _ string, _ io.Reader, _, _ io.Writer) (int, error) {
		return 0, fmt.Errorf("%s: %w", argv[0], exec.ErrNotFound)
	}
	var out bytes.Buffer
	if code, _ := Run(context.Background(), `exec grok --x`, Options{Stdout: &out, Stderr: &out, Start: notFound}); code != 127 {
		t.Fatalf("code = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "mewla sh: grok: "+exec.ErrNotFound.Error() {
		t.Fatalf("printed %q", got)
	}
	out.Reset()
	refused := func(context.Context, []string, []string, string, io.Reader, io.Writer, io.Writer) (int, error) {
		return 0, errors.New("access is denied")
	}
	Run(context.Background(), `agent`, Options{Stdout: &out, Stderr: &out, Start: refused})
	if got := strings.TrimSpace(out.String()); got != "mewla sh: agent: access is denied" {
		t.Fatalf("printed %q", got)
	}
}
