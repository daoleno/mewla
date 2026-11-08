//go:build linux

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

// LinuxBackend runs the agent browser's Chrome on the agent screen (see
// package vdisplay): a display of its own with a monitor for each agent, like
// every other program an agent runs. It is never the user's seat or profile.
type LinuxBackend struct {
	Chrome string
	// Prepare gets the profile's user data directory before Chrome starts. It
	// registers Mewla's native messaging host there (Chrome looks in the user
	// data directory first) and returns the folder of the Mewla extension,
	// which is loaded into Chrome each time it starts.
	Prepare func(userDataDir string) (extensionDir string, err error)
	// Screens is the agent screen Chrome runs on.
	Screens Screens
}

// NewBackend is the Linux backend. prepare registers Mewla's native messaging
// host in the profile and returns the extension's folder (see LinuxBackend).
func NewBackend(prepare func(userDataDir string) (extensionDir string, err error), screens Screens) Backend {
	return &LinuxBackend{Prepare: prepare, Screens: screens}
}

func (b *LinuxBackend) chrome() string {
	if b.Chrome != "" {
		return b.Chrome
	}
	return findChrome()
}

// Capability is whether this computer can run the agent browser, and why not:
// a Chrome, and the agent screen's X server.
func (b *LinuxBackend) Capability(ctx context.Context) Capability {
	if b.chrome() == "" {
		return Capability{Reason: "Install a supported Chrome or Chromium browser on this server"}
	}
	if err := vdisplay.Capability(); err != nil {
		return Capability{Reason: err.Error()}
	}
	return Capability{Available: true}
}

func findChrome() string {
	for _, p := range []string{"/opt/google/chrome/chrome", "chromium", "google-chrome-stable"} {
		if q, e := exec.LookPath(p); e == nil {
			return q
		}
	}
	return ""
}
func commandOutput(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	var out limitedOutput
	cmd.Stdout = &out
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("Browser response exceeded its limit")
	}
	return out.Bytes(), err
}

// Discard beyond the bound while draining the subprocess pipe.
type limitedOutput struct {
	bytes.Buffer
	overflow bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remain := (2 << 20) - w.Len()
	if len(p) > remain {
		w.overflow = true
		p = p[:remain]
	}
	_, _ = w.Buffer.Write(p)
	return n, nil
}

var secretServiceReady = secretServiceProbe

const (
	storeSecretService = "gnome-libsecret"
	storeBasic         = "basic"
)

// secretServiceReady (the probe, a variable so tests can stand in a wallet) says whether the server user's default wallet is running
// and unlocked, or what is missing. NameHasOwner does not activate or unlock a
// personal keyring.
func secretServiceProbe(ctx context.Context) (bool, string) {
	if _, e := exec.LookPath("gdbus"); e != nil {
		return false, "gdbus is not installed on this server"
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus", "--method", "org.freedesktop.DBus.NameHasOwner", "org.freedesktop.secrets")
	if err != nil || !strings.Contains(string(raw), "true") {
		return false, "A secure wallet is not running. Start and unlock KWallet or another Secret Service for the server user"
	}
	raw, err = commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", "/org/freedesktop/secrets", "--method", "org.freedesktop.Secret.Service.ReadAlias", "default")
	if err != nil {
		return false, "The server's secure keyring is unavailable"
	}
	// gdbus's object-path result is a typed D-Bus tuple, not a shell command.
	parts := strings.Split(string(raw), "'")
	if len(parts) < 3 || !strings.HasPrefix(parts[1], "/org/freedesktop/secrets/collection/") {
		return false, "Create a default collection in the server user's secure keyring"
	}
	raw, err = commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", parts[1], "--method", "org.freedesktop.DBus.Properties.Get", "org.freedesktop.Secret.Collection", "Locked")
	if err != nil || !strings.Contains(string(raw), "false") {
		return false, "Unlock the server user's default wallet through its normal KWallet/Secret Service setup"
	}
	return true, ""
}

// credentialStore picks how Chrome keeps this profile's sign-ins: the Secret
// Service when it works, else Chrome's own basic storage (the profile folder
// is private to the server user). A profile keeps the store it started with,
// since cookies written under one are not readable under the other, so one
// that began on the wallet waits for the wallet rather than losing its sign-ins.
func credentialStore(ctx context.Context, root string) (string, error) {
	pin := filepath.Join(root, "credential-store")
	if raw, err := os.ReadFile(pin); err == nil {
		switch strings.TrimSpace(string(raw)) {
		case storeBasic:
			return storeBasic, nil
		case storeSecretService:
			if ok, reason := secretServiceReady(ctx); !ok {
				return "", errors.New(reason + ", then open this browser again; its sign-ins are kept with the wallet")
			}
			return storeSecretService, nil
		}
	}
	store := storeBasic
	if ok, _ := secretServiceReady(ctx); ok {
		store = storeSecretService
	}
	if err := atomicfile.Write(pin, []byte(store+"\n"), 0o600); err != nil {
		return "", err
	}
	return store, nil
}

type child struct {
	cmd    *exec.Cmd
	done   chan struct{}
	stderr *tailBuffer
}

// tailBuffer keeps the last few KiB a process wrote, so a launch that failed
// can say why instead of only that it did.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailLimit = 2048

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailLimit {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailLimit:]...)
	}
	return len(p), nil
}

// reason is the last lines the process said, as one short string.
func (p *child) reason() string {
	if p == nil || p.stderr == nil {
		return ""
	}
	p.stderr.mu.Lock()
	text := strings.TrimSpace(strings.ToValidUTF8(string(p.stderr.buf), ""))
	p.stderr.mu.Unlock()
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	text = strings.Join(lines, " | ")
	if len(text) > 600 {
		text = text[len(text)-600:]
	}
	return text
}

// launchFailure names the step that failed and why: what the process said, or
// how the wait ended.
func launchFailure(step string, cause error, p *child) error {
	detail := p.reason()
	if detail == "" {
		detail = cause.Error()
	} else if p != nil && !p.alive() {
		detail = fmt.Sprintf("exited (%v): %s", p.cmd.ProcessState, detail)
	}
	return fmt.Errorf("%s: %s", step, detail)
}

func spawn(env []string, name string, args ...string) (*child, error) {
	return spawnWith(env, nil, name, args...)
}

// spawnWith starts a process that also gets extra files as its descriptors 3, 4, ….
func spawnWith(env []string, extra []*os.File, name string, args ...string) (*child, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.ExtraFiles = extra
	tail := &tailBuffer{}
	cmd.Stderr = tail
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &child{cmd: cmd, done: make(chan struct{}), stderr: tail}
	go func() { _ = cmd.Wait(); close(p.done) }()
	return p, nil
}
func (p *child) alive() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (p *child) stop() {
	if !p.alive() {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}

// linuxHost is the computer's one Chrome on the agent profile, with a private
const ownerEnv = "MEWLA_BROWSER_OWNER"

// tempPrefix starts the private run directory of every runtime.
const tempPrefix = "zb-"

func waitForFile(ctx context.Context, d time.Duration, dir string, creator *child, ready func() bool) error {
	watcher, err := fsnotify.NewWatcher()
	var events <-chan fsnotify.Event
	var watchErrors <-chan error
	var tick <-chan time.Time
	if err == nil {
		defer watcher.Close()
		err = watcher.Add(dir)
	}
	if err == nil {
		events, watchErrors = watcher.Events, watcher.Errors
	} else {
		// No inotify (limits exhausted): poll, and say so once per launch.
		log.Printf("browser: cannot watch %s for the launch (%v); polling every 50ms", dir, err)
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		tick = t.C
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	var exited <-chan struct{}
	if creator != nil {
		exited = creator.done
	}
	// The watch is in place before the first check, so a file created between
	// the two is not missed.
	for !ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return context.DeadlineExceeded
		case <-exited:
			if ready() {
				return nil
			}
			return errors.New("the process exited before it was ready")
		case <-events:
		case <-watchErrors:
			// An overflowed queue only means events were missed; ready() looks.
		case <-tick:
		}
	}
	return nil
}

// waitForExit waits for a process to end, up to d.
func waitForExit(ctx context.Context, d time.Duration, p *child) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return context.DeadlineExceeded
	}
}

// linuxHost is the computer's one Chrome on the agent profile, running on the
// agent screen. The extension opens an agent's windows in it and places each
// on its agent's monitor; nothing here touches a window.
type linuxHost struct {
	mu     sync.Mutex
	rundir string
	chrome *child
	lock   *os.File
	closed bool
	pipe   *pipeCDP
}

func (b *LinuxBackend) Start(ctx context.Context, root string) (Host, error) {
	if c := b.Capability(ctx); !c.Available {
		return nil, errors.New(c.Reason)
	}
	if b.Screens == nil {
		return nil, errors.New("The agent screen is not available")
	}
	reapStrays()
	if err := privateDir(root); err != nil {
		return nil, err
	}
	profile := filepath.Join(root, "profile")
	if err := privateDir(profile); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "runtime.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("The agent browser profile already has a runtime owner")
	}
	store, err := credentialStore(ctx, root)
	if err != nil {
		lock.Close()
		return nil, err
	}
	number, xauth, err := b.Screens.DisplayEnv(ctx)
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("The agent screen could not start: %w", err)
	}
	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		lock.Close()
		return nil, err
	}
	h := &linuxHost{rundir: dir, lock: lock}
	success := false
	defer func() {
		if !success {
			_ = h.Close()
		}
	}()
	for _, n := range []string{"run", "config", "data", "cache"} {
		if err = privateDir(filepath.Join(dir, n)); err != nil {
			return nil, err
		}
	}
	// Chrome keeps the daemon user's stable Secret Service bus (gdbus finds it
	// through the original XDG_RUNTIME_DIR, which Chrome does not get).
	bus := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if bus == "" {
		userRuntime := os.Getenv("XDG_RUNTIME_DIR")
		if userRuntime == "" {
			userRuntime = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		bus = "unix:path=" + filepath.Join(userRuntime, "bus")
	}
	env := []string{}
	for _, v := range os.Environ() {
		k := strings.SplitN(v, "=", 2)[0]
		if k == "DBUS_SESSION_BUS_ADDRESS" || k == "DISPLAY" || k == "XAUTHORITY" || k == "WAYLAND_DISPLAY" || k == "SESSION_MANAGER" || k == ownerEnv || strings.HasPrefix(k, "XDG_") {
			continue
		}
		env = append(env, v)
	}
	// ownerEnv marks every process of this runtime with the daemon that started
	// it, so a later daemon can tell what a crash left behind.
	env = append(env, "DBUS_SESSION_BUS_ADDRESS="+bus, ownerEnv+"="+strconv.Itoa(os.Getpid()), "XDG_SESSION_TYPE=x11", "XDG_RUNTIME_DIR="+filepath.Join(dir, "run"), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), fmt.Sprintf("DISPLAY=:%d", number), "XAUTHORITY="+xauth)
	// The private profile lock is also enforced by Chrome. Never delete its SingletonLock.
	extension := ""
	if b.Prepare != nil {
		if extension, err = b.Prepare(profile); err != nil {
			return nil, fmt.Errorf("Mewla's extension could not be prepared: %w", err)
		}
	}
	// Chrome 137 and later ignore --load-extension. An extension can still be
	// loaded over the debugging pipe, which is also what lets nobody but this
	// daemon do it: the pipe's other end is ours, and Chrome ends if it closes.
	toChromeR, toChromeW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fromChromeR, fromChromeW, err := os.Pipe()
	if err != nil {
		toChromeR.Close()
		toChromeW.Close()
		return nil, err
	}
	// Despite Chrome's historical flag name, libsecret is the standard Secret
	// Service client, also supported by installed KDE ksecretd/KWallet. The
	// windows of different agents are on different monitors, but a window
	// that is covered or off screen must not be slowed down for it.
	h.chrome, err = spawnWith(env, []*os.File{toChromeR, fromChromeW}, b.chrome(), "--user-data-dir="+profile, "--remote-debugging-pipe", "--enable-unsafe-extension-debugging", "--silent-debugger-extension-api", "--ozone-platform=x11", "--password-store="+store, "--no-first-run", "--no-default-browser-check", "--no-startup-window", "--disable-features=OutdatedBuildDetector", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling")
	toChromeR.Close()
	fromChromeW.Close()
	if err != nil {
		toChromeW.Close()
		fromChromeR.Close()
		return nil, err
	}
	h.pipe = newPipeCDP(toChromeW, fromChromeR)
	if _, err = h.pipe.call(ctx, "Browser.getVersion", nil); err != nil {
		return nil, launchFailure("Managed Chrome could not start", err, h.chrome)
	}
	if extension != "" {
		raw, err := h.pipe.call(ctx, "Extensions.loadUnpacked", map[string]any{"path": extension})
		if err != nil {
			return nil, launchFailure("Mewla's extension could not be loaded into the agent browser", err, h.chrome)
		}
		var loaded struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &loaded)
		if loaded.ID == "" {
			return nil, launchFailure("Mewla's extension could not be loaded into the agent browser", errors.New("Chrome returned no extension ID"), h.chrome)
		}
	}
	success = true
	return h, nil
}

func (h *linuxHost) Alive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.chrome.alive()
}

func (h *linuxHost) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	// Ask Chrome to quit, so it flushes its persistent profile (the agents'
	// sign-ins) before anything is terminated: SIGTERM to the whole process
	// group can kill the cookie writer too early.
	if h.chrome.alive() && h.pipe != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_, _ = h.pipe.call(ctx, "Browser.close", nil)
		_ = waitForExit(ctx, 6*time.Second, h.chrome)
		cancel()
	}
	h.chrome.stop()
	if h.pipe != nil {
		h.pipe.close()
	}
	if h.lock != nil {
		_ = unix.Flock(int(h.lock.Fd()), unix.LOCK_UN)
		_ = h.lock.Close()
	}
	if h.rundir != "" {
		_ = os.RemoveAll(h.rundir)
	}
	return nil
}

// pipeCDP speaks to Chrome over its debugging pipe: NUL-delimited JSON, ours
// to read for as long as Chrome runs (it would block on a full pipe).
type pipeCDP struct {
	out, in *os.File
	mu      sync.Mutex
	next    int
	pending map[int]chan pipeReply
	ended   bool
}

type pipeReply struct {
	result json.RawMessage
	err    error
}

func newPipeCDP(out, in *os.File) *pipeCDP {
	p := &pipeCDP{out: out, in: in, pending: map[int]chan pipeReply{}}
	go p.read()
	return p
}

func (p *pipeCDP) read() {
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := p.in.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for {
			i := bytes.IndexByte(buf, 0)
			if i < 0 {
				break
			}
			msg := buf[:i]
			buf = buf[i+1:]
			var m struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(msg, &m) != nil || m.ID == 0 {
				continue
			}
			p.mu.Lock()
			ch := p.pending[m.ID]
			delete(p.pending, m.ID)
			p.mu.Unlock()
			if ch != nil {
				if m.Error.Message != "" {
					ch <- pipeReply{err: errors.New(m.Error.Message)}
				} else {
					ch <- pipeReply{result: m.Result}
				}
			}
		}
		if len(buf) > 1<<20 {
			buf = buf[:0]
		}
		if err != nil {
			p.mu.Lock()
			p.ended = true
			for id, ch := range p.pending {
				ch <- pipeReply{err: errors.New("Chrome closed its debugging pipe")}
				delete(p.pending, id)
			}
			p.mu.Unlock()
			return
		}
	}
}

func (p *pipeCDP) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	ch := make(chan pipeReply, 1)
	p.mu.Lock()
	if p.ended {
		p.mu.Unlock()
		return nil, errors.New("Chrome closed its debugging pipe")
	}
	p.next++
	id := p.next
	p.pending[id] = ch
	p.mu.Unlock()
	req, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if _, err := p.out.Write(append(req, 0)); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("Chrome did not answer")
	}
}

func (p *pipeCDP) close() {
	_ = p.out.Close()
	_ = p.in.Close()
}
