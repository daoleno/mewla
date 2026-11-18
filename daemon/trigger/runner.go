package trigger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

// Result is what one check did.
type Result struct {
	Stdout     string
	Truncated  bool
	StderrTail string
	Exit       int
	// Err is why the check could not run or finish (start failure, timeout).
	Err      string
	Duration time.Duration
}

func (r Result) Failed() bool { return r.Err != "" || r.Exit != 0 }

// Failure is the one-paragraph reason a failed check gives.
func (r Result) Failure() string {
	reason := r.Err
	if reason == "" {
		reason = fmt.Sprintf("exit status %d", r.Exit)
	}
	if tail := strings.TrimSpace(r.StderrTail); tail != "" {
		reason += "\n" + tail
	}
	return reason
}

// RunFunc runs one check; tests replace it.
type RunFunc func(ctx context.Context, t Trigger) Result

// ShellArgv is the program and arguments that run check on goos: /bin/sh
// on Unix; on Windows PowerShell 7 (pwsh) when installed, otherwise the
// Windows PowerShell every Windows has.
func ShellArgv(goos string, lookPath func(string) (string, error), check string) []string {
	if goos != "windows" {
		return []string{"/bin/sh", "-c", check}
	}
	shell := "powershell.exe"
	if path, err := lookPath("pwsh"); err == nil && path != "" {
		shell = path
	}
	return []string{shell, "-NoProfile", "-NonInteractive", "-Command", check}
}

// CheckEnv is the environment a check runs with: the daemon's, with PATH
// resolved the way a Session's shell sees it (hostdeps.UserPATH), so a tool
// in ~/.local/bin or a mise shim is found even when launchd or systemd
// started the daemon with a bare PATH.
func CheckEnv(base []string, triggerID string) []string {
	env := make([]string, 0, len(base)+4)
	path := ""
	hasHome := false
	for _, entry := range base {
		key, value, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "PATH":
			path = value
			continue
		case "HOME":
			hasHome = true
		case "TERM", "MEWLA_TRIGGER_ID":
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+hostdeps.UserPATH(path), "TERM=dumb", "MEWLA_TRIGGER_ID="+triggerID)
	if !hasHome {
		if home, err := os.UserHomeDir(); err == nil {
			env = append(env, "HOME="+home)
		}
	}
	return env
}

// RunCheck runs t's check in its own process group with no input, keeps
// MaxOutputBytes of stdout and the last MaxStderrBytes of stderr, and kills
// the whole group at the timeout.
func RunCheck(ctx context.Context, t Trigger) Result {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout())
	defer cancel()
	argv := ShellArgv(runtime.GOOS, exec.LookPath, t.Check)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = t.Cwd
	if cmd.Dir == "" {
		cmd.Dir, _ = os.UserHomeDir()
	}
	cmd.Env = CheckEnv(os.Environ(), t.ID)
	stdout := &capWriter{limit: MaxOutputBytes}
	stderr := &tailWriter{limit: MaxStderrBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	isolate(cmd)
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	err := cmd.Run()
	result := Result{
		Stdout: stdout.buf.String(), Truncated: stdout.truncated,
		StderrTail: stderr.String(), Duration: time.Since(started),
	}
	var exit *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		result.Err = fmt.Sprintf("timed out after %s", t.Timeout())
		result.Exit = -1
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		result.Exit = exit.ExitCode()
	default:
		result.Err = err.Error()
		result.Exit = -1
	}
	return result
}

type capWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

type tailWriter struct {
	buf   []byte
	limit int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - w.limit; over > 0 {
		w.buf = append([]byte(nil), w.buf[over:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.buf) }
