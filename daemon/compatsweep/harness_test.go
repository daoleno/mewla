//go:build linux || darwin

package compatsweep

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/gorilla/websocket"
)

// sweepBinary is the mewla binary under test (built with -tags ghosttyvt, or
// its session host exits at start and every scenario looks broken).
func sweepBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("MEWLA_SWEEP_BINARY")
	if bin == "" {
		t.Skip("set MEWLA_SWEEP_BINARY to a mewla binary built with -tags ghosttyvt")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// env is one fresh computer for one scenario: its own HOME, state, port,
// tmux directory and PATH. It never sees the person's daemon, tmux server,
// systemd units or agent configs: a systemd-run stub keeps the session host a
// plain child, TMUX_TMPDIR points tmux at an empty directory, and every agent
// config is a copy.
type env struct {
	t     *testing.T
	bin   string
	dir   string
	home  string
	state string
	tools string // first on PATH: stubs and the agents of this scenario
	port  int
	path  []string
	extra []string
	serve *exec.Cmd
	log   string
}

type envOptions struct {
	// path is put after tools on PATH; nil is the system's /usr/bin:/bin.
	path []string
	// env is extra environment for the daemon (NAME=value).
	env []string
	// executors is the executors.toml content; empty writes none.
	executors string
	// hostExecutor locks Brain's agent (MEWLA_BRAIN_HOST_EXECUTOR).
	hostExecutor string
	// gateway passes this shell's ANTHROPIC_* and OPENAI_* variables, for
	// scenarios with a real agent.
	gateway bool
}

func newEnv(t *testing.T, opts envOptions) *env {
	t.Helper()
	bin := sweepBinary(t)
	dir, err := os.MkdirTemp("", "msw")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, bin: bin, dir: dir, home: filepath.Join(dir, "home"), tools: filepath.Join(dir, "tools"), log: filepath.Join(dir, "daemon.log")}
	e.state = filepath.Join(e.home, ".mewla")
	for _, sub := range []string{e.state, e.tools, filepath.Join(dir, "tmux"), filepath.Join(dir, "tmp")} {
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// zsh's new-user wizard would swallow a typed command; bash reads nothing.
	for _, rc := range []string{".zshrc", ".bashrc", ".profile"} {
		writeFile(t, filepath.Join(e.home, rc), "", 0o600)
	}
	writeFile(t, filepath.Join(e.tools, "systemd-run"), "#!/bin/sh\nexit 1\n", 0o755)
	if opts.executors != "" {
		writeFile(t, filepath.Join(e.state, "executors.toml"), opts.executors, 0o600)
	}
	e.path = append([]string{e.tools}, opts.path...)
	if opts.path == nil {
		e.path = append(e.path, "/usr/bin", "/bin")
	}
	e.extra = append(e.extra, opts.env...)
	if opts.hostExecutor != "" {
		e.extra = append(e.extra, "MEWLA_BRAIN_HOST_EXECUTOR="+opts.hostExecutor)
	}
	if opts.gateway {
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "OPENAI_") {
				e.extra = append(e.extra, kv)
			}
		}
	}
	e.port = freePort(t)
	t.Cleanup(e.close)
	return e
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func (e *env) environ() []string {
	return append([]string{
		"HOME=" + e.home,
		"PATH=" + strings.Join(e.path, ":"),
		"TERM=xterm-256color",
		"SHELL=/bin/sh",
		"LANG=C.UTF-8",
		"TMUX_TMPDIR=" + filepath.Join(e.dir, "tmux"),
		// Its own: Workers refuse a folder under TMPDIR (volatile storage).
		"TMPDIR=" + filepath.Join(e.dir, "tmp"),
		"MEWLA_NO_AUTOUPDATE=1",
	}, e.extra...)
}

// start runs `mewla serve` and waits for /health.
func (e *env) start() {
	e.t.Helper()
	logFile, err := os.OpenFile(e.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		e.t.Fatal(err)
	}
	cmd := exec.Command(e.bin, "serve", "-addr", fmt.Sprintf("127.0.0.1:%d", e.port), "-state-dir", e.state, "-loopback-only")
	cmd.Dir = e.home
	cmd.Env = e.environ()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.serve = cmd
	go func() { _ = cmd.Wait(); logFile.Close() }()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(e.url() + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	e.t.Fatalf("daemon did not answer /health on %d:\n%s", e.port, e.logTail(40))
}

func (e *env) url() string { return fmt.Sprintf("http://127.0.0.1:%d", e.port) }

// stopServe ends `mewla serve` only (the session host and its Sessions keep
// running, as on a real restart).
func (e *env) stopServe() {
	if e.serve == nil || e.serve.Process == nil {
		return
	}
	_ = e.serve.Process.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if e.serve.ProcessState != nil || syscall.Kill(e.serve.Process.Pid, 0) != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(e.serve.Process.Pid, syscall.SIGKILL)
	e.serve = nil
}

// close stops everything this environment started: every process whose HOME
// is this environment's (the daemon, its session host, the agents), then
// removes the scratch directory unless MEWLA_SWEEP_KEEP is set.
func (e *env) close() {
	e.stopServe()
	for round := 0; round < 3; round++ {
		pids := e.processes()
		if len(pids) == 0 {
			break
		}
		for _, pid := range pids {
			sig := syscall.SIGTERM
			if round > 0 {
				sig = syscall.SIGKILL
			}
			_ = syscall.Kill(pid, sig)
		}
		time.Sleep(time.Second)
	}
	if e.t.Failed() || os.Getenv("MEWLA_SWEEP_KEEP") != "" {
		e.t.Logf("kept %s", e.dir)
		return
	}
	// The scratch dir holds pairing state and credential copies.
	_ = os.RemoveAll(e.dir)
}

// processes are this user's processes whose HOME is this environment's.
func (e *env) processes() []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	want := []byte("HOME=" + e.home + "\x00")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil {
			continue
		}
		if bytes.HasPrefix(raw, want) || bytes.Contains(raw, append([]byte{0}, want...)) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// cli runs a mewla command against this environment's daemon.
func (e *env) cli(timeout time.Duration, args ...string) (string, error) {
	e.t.Helper()
	// "@state" places -state-dir (flags must precede positional arguments);
	// without it the flag goes last.
	var full []string
	placed := false
	for _, arg := range args {
		if arg == "@state" {
			full, placed = append(full, "-state-dir", e.state), true
			continue
		}
		full = append(full, arg)
	}
	if !placed {
		full = append(full, "-state-dir", e.state)
	}
	cmd := exec.Command(e.bin, full...)
	cmd.Dir = e.home
	cmd.Env = e.environ()
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return string(out), err
}

func (e *env) logText() string {
	raw, _ := os.ReadFile(e.log)
	return string(raw)
}

func (e *env) logTail(lines int) string {
	all := strings.Split(strings.TrimRight(e.logText(), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// waitLog waits for a daemon log line matching pattern.
func (e *env) waitLog(pattern string, timeout time.Duration) (string, bool) {
	re := regexp.MustCompile(pattern)
	deadline := time.Now().Add(timeout)
	for {
		scanner := bufio.NewScanner(strings.NewReader(e.logText()))
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			if re.MatchString(scanner.Text()) {
				return scanner.Text(), true
			}
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// --- a paired client, as the app pairs and connects -----------------------

type device struct {
	ID       string
	DaemonID string
	key      ed25519.PrivateKey
}

var pairLinkRe = regexp.MustCompile(`mewla://settings\?(?:v=\d+&)?p=[A-Za-z0-9_\-]+`)

func (e *env) pair() device {
	e.t.Helper()
	out, err := e.cli(30*time.Second, "pair", "-details")
	if decoded, decodeErr := url.QueryUnescape(out); decodeErr == nil {
		out = decoded
	}
	link := pairLinkRe.FindString(out)
	if link == "" {
		e.t.Fatalf("no pairing link (err %v)", err)
	}
	parsed, _ := url.Parse(link)
	raw, err := base64.RawURLEncoding.DecodeString(parsed.Query().Get("p"))
	if err != nil || len(raw) < 3 {
		e.t.Fatalf("pairing payload: %v", err)
	}
	urlLen := int(binary.BigEndian.Uint16(raw[1:3]))
	rest := raw[3+urlLen:]
	daemonKey, token := hex.EncodeToString(rest[:32]), hex.EncodeToString(rest[32:])
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		e.t.Fatal(err)
	}
	deviceID := "sweep-" + hex.EncodeToString(public[:6])
	body, _ := json.Marshal(map[string]any{
		"enrollment_token": token, "expected_daemon_public_key": daemonKey,
		"device_id": deviceID, "device_name": "Sweep browser", "device_public_key": hex.EncodeToString(public),
	})
	response, err := http.Post(e.url()+"/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer response.Body.Close()
	var reply map[string]any
	_ = json.NewDecoder(response.Body).Decode(&reply)
	if response.StatusCode != http.StatusOK {
		e.t.Fatalf("pair: %d %v", response.StatusCode, reply)
	}
	daemonID, _ := reply["daemon_id"].(string)
	return device{ID: deviceID, DaemonID: daemonID, key: private}
}

// client is one WebSocket connection; every message it receives is kept.
type client struct {
	t    *testing.T
	conn *websocket.Conn
	mu   sync.Mutex
	seen []map[string]any
	wake chan struct{}
}

func (e *env) connect(d device) *client {
	e.t.Helper()
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(d.key, auth.BuildSignaturePayload("mewla-connect", d.DaemonID, d.ID, timestamp, nonceHex))
	header := http.Header{}
	header.Set("Authorization", auth.AuthorizationHeaderPrefix+"v1:"+d.ID+":"+d.DaemonID+":"+timestamp+":"+nonceHex+":"+hex.EncodeToString(signature))
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(e.url(), "http", "ws", 1)+"/ws", header)
	if err != nil {
		e.t.Fatal(err)
	}
	c := &client{t: e.t, conn: conn, wake: make(chan struct{}, 1)}
	go func() {
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var message map[string]any
			if json.Unmarshal(payload, &message) != nil {
				continue
			}
			c.mu.Lock()
			c.seen = append(c.seen, message)
			c.mu.Unlock()
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}
	}()
	e.t.Cleanup(func() { conn.Close() })
	return c
}

func (c *client) send(message map[string]any) {
	c.t.Helper()
	raw, _ := json.Marshal(message)
	if err := c.conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		c.t.Fatal(err)
	}
}

// waitFor returns the first message (kept or arriving) that match accepts.
func (c *client) waitFor(timeout time.Duration, match func(map[string]any) bool) (map[string]any, bool) {
	deadline := time.Now().Add(timeout)
	next := 0
	for {
		c.mu.Lock()
		for ; next < len(c.seen); next++ {
			if match(c.seen[next]) {
				found := c.seen[next]
				c.mu.Unlock()
				return found, true
			}
		}
		c.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			return nil, false
		}
		select {
		case <-c.wake:
		case <-time.After(min(left, time.Second)):
		}
	}
}

func (c *client) types() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for _, message := range c.seen {
		kind, _ := message["type"].(string)
		out[kind]++
	}
	return out
}

// path walks a decoded JSON value: path(m, "brain", "host_problem", "code").
func path(value any, keys ...string) any {
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func str(value any) string {
	text, _ := value.(string)
	return text
}

// --- results ---------------------------------------------------------------

// record appends one scenario row to $MEWLA_SWEEP_OUT/results.jsonl.
func record(t *testing.T, scenario, outcome, detail string) {
	t.Logf("%s: %s: %s", scenario, outcome, detail)
	dir := os.Getenv("MEWLA_SWEEP_OUT")
	if dir == "" {
		return
	}
	raw, _ := json.Marshal(map[string]string{"scenario": scenario, "outcome": outcome, "detail": detail, "at": time.Now().UTC().Format(time.RFC3339)})
	file, err := os.OpenFile(filepath.Join(dir, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}

// hold keeps the scenario's daemon up for a look in the web UI when
// MEWLA_SWEEP_HOLD names a file: the browser link is written there, and the
// scenario ends when the file is removed (or after 20 minutes).
func (e *env) hold() {
	file := os.Getenv("MEWLA_SWEEP_HOLD")
	if file == "" {
		return
	}
	link, err := e.cli(30*time.Second, "web", "-print", "@state")
	if err != nil {
		e.t.Logf("mewla web -print: %v: %s", err, link)
		return
	}
	writeFile(e.t, file, link, 0o600)
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(file); err != nil {
			return
		}
		time.Sleep(time.Second)
	}
}
