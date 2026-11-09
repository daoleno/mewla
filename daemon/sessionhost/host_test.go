//go:build linux || darwin

package sessionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// fakeScreen stands in for the terminal engine: it keeps the raw output,
// answers a primary device attributes query and follows mode 2004.
type fakeScreen struct {
	mu        sync.Mutex
	out       bytes.Buffer
	bracketed bool
	cols      int
	rows      int
}

func newFakeScreen(opts ghosttyvt.Options) (screen, error) {
	return &fakeScreen{cols: opts.Cols, rows: opts.Rows}, nil
}

func (f *fakeScreen) Write(p []byte) ghosttyvt.Output {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out.Write(p)
	var out ghosttyvt.Output
	if bytes.Contains(p, []byte("\x1b[?2004h")) {
		f.bracketed = true
	}
	if bytes.Contains(p, []byte("\x1b[?2004l")) {
		f.bracketed = false
	}
	if bytes.Contains(p, []byte("\x1b[c")) {
		out.Reply = []byte("\x1b[?62;22c")
	}
	if bytes.Contains(p, []byte("STATUS:working")) {
		out.Status = append(out.Status, ghosttyvt.Status{State: ghosttyvt.StatusWorking, App: "fake"})
	}
	return out
}

func (f *fakeScreen) Resize(cols, rows int) (ghosttyvt.Output, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	return ghosttyvt.Output{}, nil
}

func (f *fakeScreen) Capture(ghosttyvt.CaptureOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String(), nil
}

func (f *fakeScreen) State() ghosttyvt.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ghosttyvt.State{Cols: f.cols, Rows: f.rows, BracketedPaste: f.bracketed}
}

// EncodeKey knows the legacy encoding of a few keys.
func (f *fakeScreen) EncodeKey(spec string) ([]byte, error) {
	if err := ghosttyvt.ValidKey(spec); err != nil {
		return nil, err
	}
	if encoded, ok := map[string]string{"Enter": "\r", "C-u": "\x15", "Escape": "\x1b", "Up": "\x1b[A", "BSpace": "\x7f"}[spec]; ok {
		return []byte(encoded), nil
	}
	if len(spec) == 1 {
		return []byte(spec), nil
	}
	return nil, fmt.Errorf("fake screen cannot encode %q", spec)
}

func (f *fakeScreen) Snapshot() ([]byte, error) { return nil, errors.New("fake") }
func (f *fakeScreen) Close()                    {}

type testHost struct {
	server *Server
	path   string
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startHost(t *testing.T, opts ServerOptions) *testHost {
	t.Helper()
	if opts.SocketPath == "" {
		opts.SocketPath = filepath.Join(shortTempDir(t), "h.sock")
	}
	if opts.newScreen == nil {
		opts.newScreen = newFakeScreen
	}
	if opts.engine == nil {
		engine := true
		opts.engine = &engine
	}
	if opts.HostVersion == "" {
		opts.HostVersion = "test-host"
	}
	if opts.Logf == nil {
		opts.Logf = func(format string, args ...any) { t.Logf("host: "+format, args...) }
	}
	server := NewServer(opts)
	ctx, cancel := context.WithCancel(context.Background())
	h := &testHost{server: server, path: opts.SocketPath, cancel: cancel, done: make(chan struct{})}
	go func() {
		h.err = server.Run(ctx)
		close(h.done)
	}()
	waitFor(t, 5*time.Second, "socket", func() bool {
		_, err := os.Stat(opts.SocketPath)
		return err == nil
	})
	t.Cleanup(h.stop)
	return h
}

func (h *testHost) stop() {
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
	}
}

func dialHost(t *testing.T, h *testHost) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, h.path, "test-client")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func shellSession(id, dir, script string) CreateParams {
	return CreateParams{
		ID:   id,
		Argv: []string{"/bin/sh", "-c", script},
		Env:  []string{"PATH=/usr/bin:/bin", "HOME=" + dir},
		Dir:  dir,
		Cols: 80,
		Rows: 24,
	}
}

func screenText(t *testing.T, c *Client, id string) string {
	t.Helper()
	capture, err := c.Capture(ctxT(t), CaptureParams{ID: id, Scope: ScopeAll})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return capture.Text
}

func waitScreen(t *testing.T, c *Client, id, want string) {
	t.Helper()
	waitFor(t, 10*time.Second, fmt.Sprintf("%q on screen", want), func() bool {
		return strings.Contains(screenText(t, c, id), want)
	})
}

func TestSocketPathIsPrivatePerStateDir(t *testing.T) {
	runtimeDir := shortTempDir(t)
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("TMPDIR", runtimeDir)
	a, err := SocketPath("/state/a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SocketPath("/state/b")
	again, _ := SocketPath("/state/a/../a")
	if a == b || a != again {
		t.Fatalf("paths a=%s b=%s again=%s", a, b, again)
	}
	if filepath.Dir(a) != filepath.Join(runtimeDir, "mewla") || !strings.HasPrefix(filepath.Base(a), "host-") {
		t.Fatalf("path %s is not under %s/mewla", a, runtimeDir)
	}
	// Computing the path creates nothing; the host makes the directory.
	if _, err := os.Lstat(filepath.Dir(a)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SocketPath left %s behind: %v", filepath.Dir(a), err)
	}
	if err := EnsureSocketDir(a); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(a))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("socket dir mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestSocketDirMustBePrivate(t *testing.T) {
	dir := shortTempDir(t)
	open := filepath.Join(dir, "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ownedPrivateDir(open, os.Getuid()); err == nil {
		t.Fatal("a 0755 directory passed as private")
	}
	link := filepath.Join(dir, "link")
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	if err := ownedPrivateDir(link, os.Getuid()); err == nil {
		t.Fatal("a symlink passed as a private directory")
	}
	if err := ownedPrivateDir(private, os.Getuid()); err != nil {
		t.Fatalf("private directory refused: %v", err)
	}
	if err := ownedPrivateDir(private, os.Getuid()+1); err == nil {
		t.Fatal("a directory of another uid passed")
	}
	// An open XDG_RUNTIME_DIR is not used, nor an open mewla directory in
	// a private one.
	t.Setenv("XDG_RUNTIME_DIR", open)
	t.Setenv("TMPDIR", open)
	path, err := SocketPath("/state/x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, open) {
		t.Fatalf("socket %s placed under an open directory", path)
	}
	if err := os.Mkdir(filepath.Join(private, "mewla"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(private, "mewla"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", private)
	t.Setenv("TMPDIR", private)
	if path, _ := SocketPath("/state/x"); strings.HasPrefix(path, private) {
		t.Fatalf("socket %s placed in an open mewla directory", path)
	}
	if err := EnsureSocketDir(filepath.Join(private, "mewla", "h.sock")); err == nil {
		t.Fatal("EnsureSocketDir accepted an open directory")
	}
}

func TestHandshakeReportsVersions(t *testing.T) {
	h := startHost(t, ServerOptions{HostVersion: "0.9.1"})
	w := dialHost(t, h).Welcome()
	if w.Protocol != ProtocolVersion || w.HostVersion != "0.9.1" || !w.Engine || w.SnapshotFormat != SnapshotFormat || w.PID != os.Getpid() || w.StartedAt.IsZero() {
		t.Fatalf("welcome = %+v", w)
	}
}

func TestHostRefusesAnotherUser(t *testing.T) {
	h := startHost(t, ServerOptions{allowPeer: func(uint32) bool { return false }})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := Dial(ctx, h.path, "intruder"); err == nil {
		c.Close()
		t.Fatal("the host answered a peer it does not allow")
	}
}

func TestClientRefusesAHostOfAnotherUser(t *testing.T) {
	h := startHost(t, ServerOptions{})
	saved := serverUIDAllowed
	serverUIDAllowed = func(uint32) bool { return false }
	defer func() { serverUIDAllowed = saved }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := Dial(ctx, h.path, "client"); err == nil {
		c.Close()
		t.Fatal("the client used a socket run by another uid")
	} else if !strings.Contains(err.Error(), "not this user") {
		t.Fatalf("error = %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := shortTempDir(t)
	info, err := c.Create(ctx, shellSession("s1", dir, `printf 'ready %s\n' "$MEWLA_SESSION_ID $TERM"; read line; printf 'got %s\n' "$line"; exit 3`))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Alive || info.PID <= 0 || info.ID != "s1" || info.Dir != dir {
		t.Fatalf("created = %+v", info)
	}
	if runtime.GOOS == "linux" {
		// The foreground process, named as tmux names it: here the shell.
		waitFor(t, 5*time.Second, "foreground command", func() bool {
			info, _ := c.Info(ctx, "s1")
			return info.Command == "sh" && info.Cwd == dir
		})
	}
	waitScreen(t, c, "s1", "ready s1 xterm-256color")
	if started, err := c.Input(ctx, "s1", []InputStep{{Bytes: []byte("hello\r")}}); err != nil || !started {
		t.Fatalf("Input = %v, %v", started, err)
	}
	waitFor(t, 10*time.Second, "exit", func() bool {
		info, err := c.Info(ctx, "s1")
		return err == nil && !info.Alive
	})
	info, _ = c.Info(ctx, "s1")
	if info.ExitCode == nil || *info.ExitCode != 3 || info.ExitedAt.IsZero() || info.OutputSeq == 0 {
		t.Fatalf("exited = %+v", info)
	}
	waitScreen(t, c, "s1", "got hello")

	seen := map[string]bool{}
	timeout := time.After(5 * time.Second)
	for !(seen[EventCreated] && seen[EventOutput] && seen[EventExited]) {
		select {
		case event := <-events:
			if event.Session == "s1" {
				seen[event.Type] = true
				if event.Type == EventExited && (event.ExitCode == nil || *event.ExitCode != 3) {
					t.Fatalf("exited event = %+v", event)
				}
			}
		case <-timeout:
			t.Fatalf("events seen: %v", seen)
		}
	}
	if _, err := c.Input(ctx, "s1", []InputStep{{Bytes: []byte("x")}}); !IsCode(err, ErrCodeExited) {
		t.Fatalf("input to an exited program: %v", err)
	}
	if _, err := c.Remove(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.List(ctx); len(list) != 0 {
		t.Fatalf("list after remove = %+v", list)
	}
	if _, err := c.Info(ctx, "s1"); !IsCode(err, ErrCodeNotFound) {
		t.Fatalf("info after remove: %v", err)
	}
}

// rawReader runs a program that switches its terminal to raw mode, says
// ready, and copies exactly n input bytes into a file.
func rawReader(t *testing.T, c *Client, id string, n int) string {
	t.Helper()
	dir := shortTempDir(t)
	out := filepath.Join(dir, "in.bin")
	script := fmt.Sprintf(`stty raw -echo; printf 'RAW-READY\n'; dd bs=1 count=%d of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, n, out)
	if _, err := c.Create(ctxT(t), shellSession(id, dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, id, "RAW-READY")
	return out
}

func readWhenDone(t *testing.T, c *Client, id, path string) []byte {
	t.Helper()
	waitScreen(t, c, id, "RAW-DONE")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInputBytesArriveExactly(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	payload := []byte("line one\nline two; -x \x1b[A 日本\r")
	out := rawReader(t, c, "raw", len(payload))
	if _, err := c.Input(ctxT(t), "raw", []InputStep{{Bytes: payload}}); err != nil {
		t.Fatal(err)
	}
	if got := readWhenDone(t, c, "raw", out); !bytes.Equal(got, payload) {
		t.Fatalf("program read %q, want %q", got, payload)
	}
}

func TestKeysAreEncodedByTheTerminal(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	want := "\x15q\x1b[A\r"
	out := rawReader(t, c, "keys", len(want))
	steps := []InputStep{{Key: "C-u"}, {Key: "q"}, {Key: "Up"}, {Key: "Enter"}}
	if _, err := c.Input(ctxT(t), "keys", steps); err != nil {
		t.Fatal(err)
	}
	if got := string(readWhenDone(t, c, "keys", out)); got != want {
		t.Fatalf("program read %q, want %q", got, want)
	}
	if _, err := c.Input(ctxT(t), "keys", []InputStep{{Key: "Enterr"}}); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := c.Input(ctxT(t), "keys", []InputStep{{Key: "Enter", Bytes: []byte("x")}}); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("a step with a key and bytes: %v", err)
	}
}

func TestPasteIsBracketedOnlyWhenAsked(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	text := "two\nlines"
	plain := rawReader(t, c, "plain", len(text))
	if _, err := c.Input(ctxT(t), "plain", []InputStep{{Paste: text}}); err != nil {
		t.Fatal(err)
	}
	if got := readWhenDone(t, c, "plain", plain); string(got) != text {
		t.Fatalf("unbracketed paste read %q", got)
	}

	// A program that asks for bracketed paste gets the markers, and markers
	// inside the text cannot close the paste early.
	tricky := "a\x1b[201~rm -rf\nb"
	want := "\x1b[200~a" + "rm -rf\nb\x1b[201~"
	dir := shortTempDir(t)
	out := filepath.Join(dir, "in.bin")
	script := fmt.Sprintf(`stty raw -echo; printf '\033[?2004hRAW-READY\n'; dd bs=1 count=%d of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, len(want), out)
	if _, err := c.Create(ctxT(t), shellSession("bracketed", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "bracketed", "RAW-READY")
	if _, err := c.Input(ctxT(t), "bracketed", []InputStep{{Paste: tricky}}); err != nil {
		t.Fatal(err)
	}
	if got := readWhenDone(t, c, "bracketed", out); string(got) != want {
		t.Fatalf("bracketed paste read %q, want %q", got, want)
	}
}

func TestInputScriptsDoNotInterleave(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	out := rawReader(t, c, "order", 8)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = c.Input(ctxT(t), "order", []InputStep{{Bytes: []byte("A1")}, {SleepMS: 300}, {Bytes: []byte("A2")}})
	}()
	time.Sleep(50 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, _ = c.Input(ctxT(t), "order", []InputStep{{Bytes: []byte("B1")}, {Bytes: []byte("B2")}})
	}()
	wg.Wait()
	// Either script may go first; neither may be split by the other.
	if got := string(readWhenDone(t, c, "order", out)); got != "A1A2B1B2" && got != "B1B2A1A2" {
		t.Fatalf("program read %q, want A1A2B1B2 or B1B2A1A2", got)
	}
}

// TestNewerClientIsServed pins the version skew contract: a daemon newer than
// its host is normal. The host answers with its own protocol and names itself
// when it lacks a method, and the client decides what to do.
func TestNewerClientIsServed(t *testing.T) {
	h := startHost(t, ServerOptions{HostVersion: "0.2.99 (old)"})
	conn, err := net.Dial("unix", h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := writeJSON(conn, Hello{Protocol: ProtocolVersion + 1, Client: "0.3.0 (new)"}); err != nil {
		t.Fatal(err)
	}
	var welcome Welcome
	if err := readJSON(conn, &welcome); err != nil {
		t.Fatalf("a newer client got no welcome: %v", err)
	}
	if welcome.Protocol != ProtocolVersion || welcome.HostVersion != "0.2.99 (old)" {
		t.Fatalf("welcome = %+v", welcome)
	}
	if err := writeJSON(conn, request{ID: 7, Method: "attach", Params: json.RawMessage(`{"id":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := readJSON(conn, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 7 || resp.Error == nil || resp.Error.Code != ErrCodeUnknownMethod ||
		!strings.Contains(resp.Error.Message, "0.2.99 (old)") || !strings.Contains(resp.Error.Message, fmt.Sprintf("protocol %d", ProtocolVersion)) {
		t.Fatalf("unknown method answer = %+v", resp)
	}
	// The connection still serves known methods.
	if err := writeJSON(conn, request{ID: 8, Method: MethodList}); err != nil {
		t.Fatal(err)
	}
	var listed response
	if err := readJSON(conn, &listed); err != nil || listed.ID != 8 || listed.Error != nil {
		t.Fatalf("list after an unknown method = %+v, %v", listed, err)
	}
}

func TestTerminalRepliesReachTheProgram(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	out := filepath.Join(dir, "reply.bin")
	script := fmt.Sprintf(`stty raw -echo; printf '\033[c'; dd bs=1 count=9 of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, out)
	if _, err := c.Create(ctxT(t), shellSession("da", dir, script)); err != nil {
		t.Fatal(err)
	}
	if got := readWhenDone(t, c, "da", out); string(got) != "\x1b[?62;22c" {
		t.Fatalf("program read %q as the device attributes reply", got)
	}
}

func TestSignalAndRemove(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	dir := shortTempDir(t)
	if _, err := c.Create(ctx, shellSession("sleeper", dir, "exec sleep 60")); err != nil {
		t.Fatal(err)
	}
	if err := c.Signal(ctx, "sleeper", SignalKill); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "kill", func() bool {
		info, _ := c.Info(ctx, "sleeper")
		return !info.Alive
	})
	info, _ := c.Info(ctx, "sleeper")
	if info.ExitCode == nil || *info.ExitCode != 137 || info.ExitSignal != SignalKill {
		t.Fatalf("killed = %+v", info)
	}
	if err := c.Signal(ctx, "sleeper", SignalInterrupt); !IsCode(err, ErrCodeExited) {
		t.Fatalf("signal to an exited program: %v", err)
	}
	if err := c.Signal(ctx, "sleeper", "bogus"); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("unknown signal: %v", err)
	}

	// Removing a running Session ends its whole process group.
	pidFile := filepath.Join(dir, "child.pid")
	if _, err := c.Create(ctx, shellSession("tree", dir, fmt.Sprintf("sleep 60 & echo $! > %s; wait", pidFile))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "child pid", func() bool {
		data, err := os.ReadFile(pidFile)
		return err == nil && len(bytes.TrimSpace(data)) > 0
	})
	data, _ := os.ReadFile(pidFile)
	var child int
	fmt.Sscan(string(data), &child)
	removed, err := c.Remove(ctx, "tree")
	if err != nil {
		t.Fatal(err)
	}
	if removed.Alive {
		t.Fatalf("removed = %+v", removed)
	}
	waitFor(t, 5*time.Second, "child gone", func() bool { return !processAlive(child) })
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	// A zombie still answers signal 0; it is gone for our purpose.
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		fields := strings.Fields(string(data))
		return len(fields) < 3 || fields[2] != "Z"
	}
	return true
}

func TestLabelsAndData(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	params := shellSession("meta", shortTempDir(t), "read x")
	params.Labels = map[string]string{"worker.created": "1", "executor": "claude-main"}
	if _, err := c.Create(ctx, params); err != nil {
		t.Fatal(err)
	}
	params.Labels["executor"] = "changed after create"
	list, err := c.List(ctx)
	if err != nil || len(list) != 1 || list[0].Labels["executor"] != "claude-main" || list[0].Labels["worker.created"] != "1" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if data, err := c.GetData(ctx, "meta", "receipts"); err != nil || data.Present {
		t.Fatalf("unset data = %+v, %v", data, err)
	}
	ledger := strings.Repeat("r", maxDataBytes)
	if err := c.SetData(ctx, "meta", "receipts", ledger); err != nil {
		t.Fatal(err)
	}
	if err := c.SetData(ctx, "meta", "receipts", ledger+"x"); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("oversized data: %v", err)
	}
	if err := c.SetData(ctx, "meta", "Bad Key", "x"); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("bad data key: %v", err)
	}
	for i := 1; i < maxDataKeys; i++ {
		if err := c.SetData(ctx, "meta", fmt.Sprintf("k%d", i), "v"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SetData(ctx, "meta", "one-too-many", "v"); !IsCode(err, ErrCodeBadRequest) {
		t.Fatalf("data key over the cap: %v", err)
	}
	if err := c.SetData(ctx, "meta", "k1", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.SetData(ctx, "meta", "one-too-many", "v"); err != nil {
		t.Fatalf("a removed key did not free its place: %v", err)
	}

	// Labels and data outlast the program and go with the Session.
	if _, err := c.Input(ctx, "meta", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "exit", func() bool {
		info, _ := c.Info(ctx, "meta")
		return !info.Alive
	})
	if data, err := c.GetData(ctx, "meta", "receipts"); err != nil || data.Value != ledger {
		t.Fatalf("data after exit: %d bytes, %v", len(data.Value), err)
	}
	if info, _ := c.Info(ctx, "meta"); info.Labels["executor"] != "claude-main" {
		t.Fatalf("labels after exit: %+v", info.Labels)
	}
	if _, err := c.Remove(ctx, "meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetData(ctx, "meta", "receipts"); !IsCode(err, ErrCodeNotFound) {
		t.Fatalf("data after remove: %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	dir := shortTempDir(t)
	good := shellSession("ok", dir, "sleep 30")
	cases := map[string]func(*CreateParams){
		"bad id":         func(p *CreateParams) { p.ID = "has space" },
		"empty argv":     func(p *CreateParams) { p.Argv = nil },
		"relative dir":   func(p *CreateParams) { p.Dir = "rel" },
		"zero size":      func(p *CreateParams) { p.Cols = 0 },
		"unknown binary": func(p *CreateParams) { p.Argv = []string{"no-such-program-mewla"} },
		"bad label key":  func(p *CreateParams) { p.Labels = map[string]string{"Has Space": "x"} },
		"long label":     func(p *CreateParams) { p.Labels = map[string]string{"k": strings.Repeat("x", maxLabelBytes+1)} },
	}
	for name, mutate := range cases {
		params := good
		params.ID = "v-" + strings.ReplaceAll(name, " ", "-")
		mutate(&params)
		if _, err := c.Create(ctx, params); !IsCode(err, ErrCodeBadRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := c.Create(ctx, good); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, good); !IsCode(err, ErrCodeExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if list, _ := c.List(ctx); len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestProgramIsFoundOnTheSessionsPath(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	tool := filepath.Join(dir, "mewla-test-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho TOOL-RAN\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	params := CreateParams{ID: "tool", Argv: []string{"mewla-test-tool"}, Env: []string{"PATH=" + dir + ":/usr/bin:/bin"}, Dir: dir, Cols: 80, Rows: 24}
	if _, err := c.Create(ctxT(t), params); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "tool", "TOOL-RAN")
}

func TestUnknownMethodIsReported(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	if err := c.call(ctxT(t), "future_method", struct{}{}, nil); !IsCode(err, ErrCodeUnknownMethod) {
		t.Fatalf("unknown method: %v", err)
	}
	// The connection is still usable.
	if _, err := c.List(ctxT(t)); err != nil {
		t.Fatal(err)
	}
}

func TestHostWithoutEngine(t *testing.T) {
	engine := false
	h := startHost(t, ServerOptions{engine: &engine})
	c := dialHost(t, h)
	if c.Welcome().Engine {
		t.Fatal("welcome claims an engine")
	}
	if _, err := c.Create(ctxT(t), shellSession("x", shortTempDir(t), "true")); !IsCode(err, ErrCodeUnavailable) {
		t.Fatalf("create without engine: %v", err)
	}
}

func TestOneHostPerSocket(t *testing.T) {
	h := startHost(t, ServerOptions{})
	second := NewServer(ServerOptions{SocketPath: h.path, newScreen: newFakeScreen, Logf: t.Logf})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := second.Run(ctx); err == nil || !strings.Contains(err.Error(), "another session host") {
		t.Fatalf("second host on the same socket: %v", err)
	}
	// The first one still serves.
	dialHost(t, h)
}

func TestLeftoverSocketIsReplaced(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "h.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h := startHost(t, ServerOptions{SocketPath: path})
	waitFor(t, 5*time.Second, "dial", func() bool {
		c, err := Dial(context.Background(), h.path, "x")
		if err == nil {
			c.Close()
		}
		return err == nil
	})
}

func TestSocketComesBackWhenRemoved(t *testing.T) {
	h := startHost(t, ServerOptions{socketCheck: 50 * time.Millisecond})
	c := dialHost(t, h)
	if _, err := c.Create(ctxT(t), shellSession("kept", shortTempDir(t), "sleep 30")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(h.path); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "socket again", func() bool {
		again, err := Dial(context.Background(), h.path, "x")
		if err != nil {
			return false
		}
		defer again.Close()
		list, err := again.List(context.Background())
		return err == nil && len(list) == 1 && list[0].ID == "kept"
	})
}

func TestShutdown(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	info, err := c.Create(ctx, shellSession("busy", shortTempDir(t), "exec sleep 60"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(ctx, false); !IsCode(err, ErrCodeBusy) {
		t.Fatalf("shutdown with a running program: %v", err)
	}
	if err := c.Shutdown(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
		if h.err != nil {
			t.Fatalf("Run: %v", h.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the host did not stop")
	}
	waitFor(t, 5*time.Second, "program gone", func() bool { return !processAlive(info.PID) })
	if _, err := os.Stat(h.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left behind: %v", err)
	}
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("a call on a stopped host succeeded")
	}
}

func TestOutputEventsAreCoalesced(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, shellSession("flood", shortTempDir(t), `i=0; while [ $i -lt 3000 ]; do echo "line $i"; i=$((i+1)); done; echo FLOOD-DONE; sleep 30`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "flood", "FLOOD-DONE")
	time.Sleep(3 * outputEventGap)
	info, _ := c.Info(ctx, "flood")
	outputs, last := 0, uint64(0)
	for drained := false; !drained; {
		select {
		case event := <-events:
			if event.Session == "flood" && event.Type == EventOutput {
				outputs++
				last = event.OutputSeq
			}
		default:
			drained = true
		}
	}
	if outputs == 0 || outputs > 200 {
		t.Fatalf("%d output events for 3000 lines", outputs)
	}
	if last != info.OutputSeq {
		t.Fatalf("last output event at %d, output is at %d", last, info.OutputSeq)
	}
}

func TestProgramStatusIsRecorded(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	ctx := ctxT(t)
	if _, err := c.Create(ctx, shellSession("st", shortTempDir(t), `echo STATUS:working; read x`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "status", func() bool {
		info, _ := c.Info(ctx, "st")
		return len(info.Status) == 1 && info.Status[0].State == "working" && info.Status[0].App == "fake"
	})
	if _, err := c.Input(ctx, "st", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "working cleared at exit", func() bool {
		info, _ := c.Info(ctx, "st")
		return !info.Alive && len(info.Status) == 0
	})
}

func TestStatusRecords(t *testing.T) {
	r := newStatusRecords()
	at := time.Unix(100, 0)
	r.apply(ghosttyvt.Status{State: ghosttyvt.StatusWorking, App: "deploy"}, at)
	r.apply(ghosttyvt.Status{ID: "us-east", State: ghosttyvt.StatusDone}, at)
	r.apply(ghosttyvt.Status{ID: "us-east/db", State: ghosttyvt.StatusBlocked, Kind: ghosttyvt.StatusKindPermission}, at)
	r.apply(ghosttyvt.Status{ID: "us-eastern", State: ghosttyvt.StatusIdle}, at)
	list := r.list()
	if len(list) != 4 || list[1].ID != "us-east" || list[1].App != "deploy" || list[2].Kind != "permission" {
		t.Fatalf("list = %+v", list)
	}
	r.apply(ghosttyvt.Status{ID: "us-east", State: ghosttyvt.StatusClear}, at)
	if list := r.list(); len(list) != 2 || list[1].ID != "us-eastern" {
		t.Fatalf("after clearing us-east: %+v", list)
	}
	r.programExited()
	if list := r.list(); len(list) != 1 || list[0].ID != "us-eastern" {
		t.Fatalf("after exit: %+v", list)
	}
	r.apply(ghosttyvt.Status{State: ghosttyvt.StatusClear}, at)
	if list := r.list(); len(list) != 0 {
		t.Fatalf("after clearing all: %+v", list)
	}
	for i := 0; i < maxStatusRecords+10; i++ {
		r.apply(ghosttyvt.Status{ID: fmt.Sprintf("r%03d", i), State: ghosttyvt.StatusIdle}, at.Add(time.Duration(i)*time.Second))
	}
	list = r.list()
	if len(list) != maxStatusRecords || list[0].ID != "r010" {
		t.Fatalf("cap kept %d records starting at %s", len(list), list[0].ID)
	}
}
