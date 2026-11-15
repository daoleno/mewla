package codex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/launchsh"
)

// A wrapped Codex launch starts its app-server in the background and then
// the TUI against it. `codex --remote` tries to connect once and exits 1 when
// the app-server is not listening yet. On Windows the app-server often lost
// that race: a Codex Session or a Brain Work's Worker exited at once ("the
// program exited before input step 3"). The launch waits for the app-server
// before it starts the TUI.
func TestAWrappedCodexLaunchStartsTheTUIOnlyOnceItsAppServerListens(t *testing.T) {
	const mewla = "/opt/mewla/bin/mewla"
	for _, tc := range []struct {
		name     string
		adapter  func(dir string) Adapter
		wantCode int
	}{
		{"without the wait the TUI loses the race", func(dir string) Adapter { return Adapter{ControlDir: dir} }, 1},
		{"with the wait the TUI connects", func(dir string) Adapter { return Adapter{ControlDir: dir, Mewla: mewla} }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A short folder: a unix socket path has a length limit.
			dir, err := os.MkdirTemp("", "cx")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			launch, err := tc.adapter(dir).PrepareLaunch("codex --dangerously-bypass-approvals-and-sandbox")
			if err != nil || launch.Control == "" {
				t.Fatalf("launch = %+v, %v", launch, err)
			}
			// /bin/sh runs this off Windows; mewla sh, which runs it here,
			// has no job control to turn off.
			script := strings.TrimPrefix(launch.Command, "set +m; ")
			if runtime.GOOS != "windows" && script == launch.Command {
				t.Fatalf("the Unix launch no longer turns off job control:\n%s", launch.Command)
			}

			stop := make(chan struct{})
			var stopOnce sync.Once
			start := func(ctx context.Context, argv, _ []string, _ string, _ io.Reader, _, stderr io.Writer) (int, error) {
				switch {
				case len(argv) > 1 && argv[0] == "codex" && argv[1] == "app-server":
					// Codex's app-server, slower to listen than its TUI is to connect.
					time.Sleep(300 * time.Millisecond)
					listener, err := net.Listen("unix", launch.Control)
					if err != nil {
						return 1, err
					}
					go func() {
						for {
							conn, err := listener.Accept()
							if err != nil {
								return
							}
							_ = conn.Close()
						}
					}()
					select {
					case <-stop:
					case <-ctx.Done():
					}
					_ = listener.Close()
					return 0, nil
				case len(argv) > 1 && argv[0] == mewla && argv[1] == "codex-server-wait":
					return WaitMain(argv[2:], stderr), nil
				case len(argv) > 1 && argv[0] == "codex" && argv[1] == "--remote":
					// The TUI: one attempt, as codex --remote makes.
					defer stopOnce.Do(func() { close(stop) })
					conn, err := net.DialTimeout("unix", strings.TrimPrefix(argv[2], "unix://"), time.Second)
					if err != nil {
						fmt.Fprintf(stderr, "Error: failed to connect to remote app server at `%s`: %v\n", argv[2], err)
						return 1, nil
					}
					_ = conn.Close()
					return 0, nil
				}
				return 127, fmt.Errorf("unexpected program %q", argv)
			}
			var stderr bytes.Buffer
			code, err := launchsh.Run(context.Background(), script, launchsh.Options{Dir: dir, Stdout: io.Discard, Stderr: &stderr, Start: start})
			stopOnce.Do(func() { close(stop) })
			if err != nil || code != tc.wantCode {
				t.Fatalf("launch exited %d (%v), want %d; stderr:\n%s", code, err, tc.wantCode, stderr.String())
			}
		})
	}
}

func TestPrepareLaunchWaitsForTheAppServerBeforeTheTUI(t *testing.T) {
	launch, err := Adapter{ControlDir: filepath.Join("state", "codex-ctl"), Mewla: `C:\Users\Me\AppData\Local\Programs\Mewla\mewla.exe`}.PrepareLaunch("codex")
	if err != nil {
		t.Fatal(err)
	}
	server := strings.Index(launch.Command, " app-server --listen ")
	wait := strings.Index(launch.Command, `'C:\Users\Me\AppData\Local\Programs\Mewla\mewla.exe' codex-server-wait `)
	tui := strings.Index(launch.Command, "exec codex --remote ")
	if server < 0 || wait < server || tui < wait {
		t.Fatalf("want app-server, then the wait, then the TUI:\n%s", launch.Command)
	}
}

func TestWaitMainGivesUpWithTheLogToRead(t *testing.T) {
	if code := WaitMain(nil, io.Discard); code != 2 {
		t.Fatalf("no socket: exit %d, want 2", code)
	}
	dir, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "codex-ctl-x.sock")
	err = WaitForServer(context.Background(), socket, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not accept a connection") {
		t.Fatalf("err = %v", err)
	}
}
