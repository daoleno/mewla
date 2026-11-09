//go:build windows && ghosttyvt && cgo

package sessionhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/launchsh"
	"github.com/daoleno/mewla/daemon/platform"
)

// asShEnv makes a copy of this test binary serve as `mewla sh`, the way the
// daemon launches a Session's script on Windows.
const asShEnv = "MEWLA_SESSIONHOST_TEST_AS_SH"

func init() {
	if os.Getenv(asShEnv) != "" && len(os.Args) > 1 && os.Args[1] == "sh" {
		os.Exit(launchsh.Main(os.Args[2:], os.Stderr))
	}
}

// The session host on Windows: programs in a pseudo console (ConPTY) with
// the real libghostty-vt terminal, over an AF_UNIX socket only this user
// may use.

func startWindowsHost(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "h.sock")
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(ServerOptions{SocketPath: socket, HostVersion: "test", Logf: t.Logf})
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("host did not stop")
		}
	})
	var client *Client
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := Dial(context.Background(), socket, "test")
		if err == nil {
			client = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial host: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() { client.Close() })
	if w := client.Welcome(); !w.Engine || w.EngineVersion != ghosttyvt.Version() {
		t.Fatalf("welcome = %+v", w)
	}
	return client
}

func windowsCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitWindowsScreen(t *testing.T, c *Client, id, want string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		capture, err := c.Capture(windowsCtx(t), CaptureParams{ID: id, Scope: ScopeAll, Trim: true, Unwrap: true})
		if err == nil {
			text = capture.Text
			if strings.Contains(text, want) {
				return text
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	info, err := c.Info(windowsCtx(t), id)
	t.Fatalf("session %s never showed %q; it shows:\n%s\ninfo: %+v %v", id, want, text, info, err)
	return ""
}

func cmdSession(id, dir string, argv ...string) CreateParams {
	return CreateParams{ID: id, Argv: argv, Env: os.Environ(), Dir: dir, Cols: 100, Rows: 30}
}

func TestWindowsHostRunsAnInteractiveProgram(t *testing.T) {
	c := startWindowsHost(t)
	ctx := windowsCtx(t)
	dir := t.TempDir()
	info, err := c.Create(ctx, cmdSession("shell", dir, "cmd.exe", "/d", "/q"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Alive || info.PID == 0 || info.Command != "cmd" {
		t.Fatalf("info = %+v", info)
	}
	if _, err := c.Input(ctx, "shell", []InputStep{{Bytes: []byte("echo typed-%MEWLA_SESSION_ID%-ok")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "shell", "typed-shell-ok")
	if err := c.Resize(ctx, "shell", 120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Input(ctx, "shell", []InputStep{{Bytes: []byte("mode con")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "shell", "120")
	capture, err := c.Capture(ctx, CaptureParams{ID: "shell", Scope: ScopeScreen})
	if err != nil || capture.Cols != 120 || capture.Rows != 40 {
		t.Fatalf("capture after resize = %+v, %v", capture, err)
	}

	// A viewer gets the screen replayed, then live output.
	attachment, err := c.Attach(ctx, AttachParams{ID: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	defer attachment.Close()
	if !strings.Contains(string(attachment.Replay), "typed-shell-ok") {
		t.Fatalf("replay lacks earlier output: %q", attachment.Replay)
	}
	if _, err := c.Input(ctx, "shell", []InputStep{{Bytes: []byte("echo live-after-attach")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	deadline := time.After(20 * time.Second)
	for !strings.Contains(seen.String(), "live-after-attach") {
		select {
		case chunk, ok := <-attachment.Output():
			if !ok {
				t.Fatalf("stream ended: %q", seen.String())
			}
			seen.Write(chunk)
		case <-deadline:
			t.Fatalf("live output never arrived: %q", seen.String())
		}
	}

	// Kill ends the program and everything it started.
	if _, err := c.Input(ctx, "shell", []InputStep{{Bytes: []byte("ping -n 60 127.0.0.1")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "shell", "Pinging")
	if err := c.Signal(ctx, "shell", SignalKill); err != nil {
		t.Fatal(err)
	}
	waitExited(t, c, "shell")
}

func TestWindowsHostReportsExitCodesAndResolvesShims(t *testing.T) {
	c := startWindowsHost(t)
	ctx := windowsCtx(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.cmd"), []byte("@echo shim says %~1\r\n@exit /b 3\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	params := cmdSession("shim", dir, "agent", "hello")
	params.Env = append(os.Environ(), "PATH="+dir+";"+os.Getenv("PATH"))
	if _, err := c.Create(ctx, params); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "shim", "shim says hello")
	info := waitExited(t, c, "shim")
	if info.ExitCode == nil || *info.ExitCode != 3 {
		t.Fatalf("exit code = %v", info.ExitCode)
	}
	if _, err := c.Create(ctx, cmdSession("missing", dir, "no-such-agent-mewla")); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("a missing program = %v", err)
	}
}

func TestWindowsHostIsOnlyForThisUser(t *testing.T) {
	c := startWindowsHost(t)
	_ = c
	same, err := platform.ProcessIsCurrentUser(os.Getpid())
	if err != nil || !same {
		t.Fatalf("this process = %v, %v", same, err)
	}
	// The System process (pid 4) is not this user's.
	if other, _ := platform.ProcessIsCurrentUser(4); other {
		t.Fatal("pid 4 counted as this user")
	}
}

func TestWindowsHostRunsALaunchScriptThroughMewlaSh(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	mewla := filepath.Join(bin, "mewla.exe")
	if err := os.WriteFile(mewla, data, 0o755); err != nil {
		t.Fatal(err)
	}
	c := startWindowsHost(t)
	ctx := windowsCtx(t)
	// The watcher's launch: origin marker, then a login shell around the
	// program.
	script := `printf '\033[8m%s\033[0m\n' 'mewla-origin 0123abcd'; exec '/bin/sh' -i -l -c 'MEWLA_WORKER_ID="${MEWLA_SESSION_ID:-$TMUX_PANE}"; export MEWLA_WORKER_ID; unset TMUX; exec cmd.exe /d /q'`
	params := cmdSession("launch", t.TempDir(), mewla, "sh", "-c", script)
	params.Env = append(os.Environ(), asShEnv+"=1", "TMUX=stale")
	if _, err := c.Create(ctx, params); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "launch", "mewla-origin 0123abcd")
	if _, err := c.Input(ctx, "launch", []InputStep{{Bytes: []byte("echo worker-%MEWLA_WORKER_ID%-tmux-%TMUX%")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "launch", "worker-launch-tmux-%TMUX%")
	// The Session is named after the program, not the launcher.
	if info, err := c.Info(ctx, "launch"); err != nil || info.Command != "cmd" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	// Ctrl+C goes to the program; the launcher stays.
	if _, err := c.Input(ctx, "launch", []InputStep{{Key: "C-c"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if _, err := c.Input(ctx, "launch", []InputStep{{Bytes: []byte("echo still-here")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	waitWindowsScreen(t, c, "launch", "\nstill-here")
	if _, err := c.Input(ctx, "launch", []InputStep{{Bytes: []byte("exit 5")}, {Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	info := waitExited(t, c, "launch")
	if info.ExitCode == nil || *info.ExitCode != 5 {
		t.Fatalf("exit code = %v", info.ExitCode)
	}
}

func waitExited(t *testing.T, c *Client, id string) Info {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		info, err := c.Info(windowsCtx(t), id)
		if err == nil && !info.Alive {
			return info
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("session %s did not exit", id)
	return Info{}
}
