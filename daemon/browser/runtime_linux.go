//go:build linux

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

// LinuxBackend uses a private graphical session, not the user's seat or profile.
// Other GUI applications can use other runtime adapters without changing leases.
type LinuxBackend struct {
	Chrome string
	// Prepare gets the profile's user data directory before Chrome starts. It
	// registers Mewla's native messaging host there (Chrome looks in the user
	// data directory first) and returns the folder of the Mewla extension,
	// which is loaded into Chrome each time it starts.
	Prepare func(userDataDir string) (extensionDir string, err error)
}

// NewBackend is the Linux backend. prepare registers Mewla's native messaging
// host in the profile and returns the extension's folder (see LinuxBackend).
func NewBackend(prepare func(userDataDir string) (extensionDir string, err error)) Backend {
	return &LinuxBackend{Prepare: prepare}
}
func (b *LinuxBackend) chrome() string {
	if b.Chrome != "" {
		return b.Chrome
	}
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
func (b *LinuxBackend) Capability(ctx context.Context) Capability {
	if b.chrome() == "" {
		return Capability{Reason: "Install a supported Chrome or Chromium browser on this server"}
	}
	for _, name := range []string{"kwin_wayland", "dbus-run-session"} {
		if _, e := exec.LookPath(name); e != nil {
			return Capability{Reason: name + " is not installed on this server"}
		}
	}
	return Capability{Available: true}
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
func (b *LinuxBackend) credentialStore(ctx context.Context, root string) (string, error) {
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
// KWin display. Agents get windows on it (linuxWindow).
type linuxHost struct {
	mu              sync.Mutex
	profile, rundir string
	endpoint        string
	env             []string
	chrome, display *child
	lock            *os.File
	closed          bool
	// pipeOut and pipeIn are our ends of Chrome's debugging pipe (to Chrome,
	// from Chrome). They stay open as long as Chrome runs.
	pipeOut, pipeIn *os.File
	extensionID     string
	pipeMu          sync.Mutex
}

func (b *LinuxBackend) Start(ctx context.Context, root string) (Host, error) {
	if c := b.Capability(ctx); !c.Available {
		return nil, errors.New(c.Reason)
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
	store, err := b.credentialStore(ctx, root)
	if err != nil {
		lock.Close()
		return nil, err
	}
	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		lock.Close()
		return nil, err
	}
	r := &linuxHost{profile: profile, rundir: dir, lock: lock}
	success := false
	defer func() {
		if !success {
			_ = r.Close()
		}
	}()
	for _, n := range []string{"run", "config", "data", "cache", "s"} {
		if err = privateDir(filepath.Join(dir, n)); err != nil {
			return nil, err
		}
	}
	// Preserve the stable user bus even when the daemon inherited no explicit
	// address (gdbus normally discovers it via the original XDG_RUNTIME_DIR).
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
		if k == "DBUS_SESSION_BUS_ADDRESS" || k == "DISPLAY" || k == "WAYLAND_DISPLAY" || k == "SESSION_MANAGER" || k == ownerEnv || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "AGENT_BROWSER_") {
			continue
		}
		env = append(env, v)
	}
	// Chrome retains the daemon user's stable Secret Service bus. KWin gets its
	// own bus through dbus-run-session and never joins the user's physical seat.
	// ownerEnv marks every process of this runtime with the daemon that started
	// it, so a later daemon can tell what a crash left behind.
	env = append(env, "DBUS_SESSION_BUS_ADDRESS="+bus, ownerEnv+"="+strconv.Itoa(os.Getpid()), "XDG_SESSION_TYPE=wayland", "XDG_RUNTIME_DIR="+filepath.Join(dir, "run"), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), "WAYLAND_DISPLAY=mewla-browser", "AGENT_BROWSER_SOCKET_DIR="+filepath.Join(dir, "s"), "AGENT_BROWSER_IDLE_TIMEOUT_MS=0", "AGENT_BROWSER_STREAM_QUALITY=65", "AGENT_BROWSER_STREAM_MAX_WIDTH=1280", "AGENT_BROWSER_STREAM_MAX_HEIGHT=800", "AGENT_BROWSER_NO_AUTO_DIALOG=1")
	r.env = env
	r.display, err = spawn(env, "dbus-run-session", "--", "kwin_wayland", "--virtual", "--width", "1280", "--height", "800", "--socket", "mewla-browser", "--no-lockscreen", "--no-global-shortcuts", "--no-kactivities")
	if err != nil {
		return nil, err
	}
	if err = waitForFile(ctx, 10*time.Second, filepath.Join(dir, "run"), r.display, func() bool {
		_, e := os.Stat(filepath.Join(dir, "run", "mewla-browser"))
		return e == nil
	}); err != nil {
		return nil, launchFailure("Private graphical session (kwin_wayland) could not start", err, r.display)
	}
	// The private profile lock is also enforced by Chrome. Never delete its SingletonLock.
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
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
	// Service client, also supported by installed KDE ksecretd/KWallet.
	// Windows of different agents overlap on one small screen; none of them may
	// be slowed down for being covered by another.
	r.chrome, err = spawnWith(env, []*os.File{toChromeR, fromChromeW}, b.chrome(), "--user-data-dir="+profile, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--remote-debugging-pipe", "--enable-unsafe-extension-debugging", "--silent-debugger-extension-api", "--ozone-platform=wayland", "--password-store="+store, "--no-first-run", "--no-default-browser-check", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling", "--window-size=1280,800", "about:blank")
	toChromeR.Close()
	fromChromeW.Close()
	if err != nil {
		toChromeW.Close()
		fromChromeR.Close()
		return nil, err
	}
	r.pipeOut, r.pipeIn = toChromeW, fromChromeR
	if err = waitForFile(ctx, 10*time.Second, profile, r.chrome, func() bool {
		data, e := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if e != nil {
			return false
		}
		port, e := strconv.Atoi(strings.Split(string(data), "\n")[0])
		if e != nil || port < 1 || port > 65535 {
			return false
		}
		r.endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
		return r.chrome.alive()
	}); err != nil {
		return nil, launchFailure("Managed Chrome could not start", err, r.chrome)
	}
	if _, err = r.pages(ctx); err != nil {
		return nil, launchFailure("Managed Chrome is up but does not answer", err, r.chrome)
	}
	if extension != "" {
		id, err := r.loadExtension(ctx, extension)
		if err != nil {
			return nil, launchFailure("Mewla's extension could not be loaded into the agent browser", err, r.chrome)
		}
		r.extensionID = id
	}
	success = true
	return r, nil
}

// ownerEnv names the daemon pid in the environment of everything a runtime starts.
const ownerEnv = "MEWLA_BROWSER_OWNER"

// tempPrefix starts the private run directory of every runtime.
const tempPrefix = "zb-"

// loadExtension loads the unpacked extension in dir over the debugging pipe
// and returns its ID. Nothing is written to the profile; the extension is
// loaded again at every start, so it is always the daemon's own version.
func (h *linuxHost) loadExtension(ctx context.Context, dir string) (string, error) {
	h.pipeMu.Lock()
	defer h.pipeMu.Unlock()
	req, _ := json.Marshal(map[string]any{"id": 1, "method": "Extensions.loadUnpacked", "params": map[string]any{"path": dir}})
	if _, err := h.pipeOut.Write(append(req, 0)); err != nil {
		return "", err
	}
	type reply struct {
		id  string
		err error
	}
	got := make(chan reply, 1)
	go func() {
		// The pipe is read to its end for as long as Chrome runs: Chrome
		// would block on a full pipe.
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 4096)
		answered := false
		for {
			n, err := h.pipeIn.Read(chunk)
			buf = append(buf, chunk[:n]...)
			for {
				i := bytes.IndexByte(buf, 0)
				if i < 0 {
					break
				}
				msg := buf[:i]
				buf = buf[i+1:]
				var m struct {
					ID     int `json:"id"`
					Result struct {
						ID string `json:"id"`
					} `json:"result"`
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if !answered && json.Unmarshal(msg, &m) == nil && m.ID == 1 {
					answered = true
					if m.Error.Message != "" {
						got <- reply{err: errors.New(m.Error.Message)}
					} else {
						got <- reply{id: m.Result.ID}
					}
				}
			}
			if len(buf) > 1<<20 {
				buf = buf[:0]
			}
			if err != nil {
				if !answered {
					got <- reply{err: err}
				}
				return
			}
		}
	}()
	select {
	case r := <-got:
		return r.id, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", errors.New("Chrome did not answer the extension load")
	}
}

func (h *linuxHost) Alive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.chrome.alive() && h.display.alive()
}

// Window is the runtime for a window of the browser that shows target. The
// extension opens and closes the window; this follows it for the person.
func (h *linuxHost) Window(ctx context.Context, target string) (Runtime, error) {
	if !h.Alive() {
		return nil, ErrUnavailable
	}
	return &linuxWindow{h: h, target: target, keys: map[string]bool{}, buttons: map[string]bool{}}, nil
}

// browserCDP sends one command to Chrome itself (not to a page).
func (h *linuxHost) browserCDP(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", h.endpoint+"/json/version", nil)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	var version struct {
		Socket string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&version)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(version.Socket)
	if err != nil || u.Hostname() != "127.0.0.1" {
		return nil, errors.New("Invalid managed browser endpoint")
	}
	return cdpCall(ctx, version.Socket, method, params)
}

// cdpCall runs one CDP command on a websocket endpoint and returns its result.
func cdpCall(ctx context.Context, endpoint, method string, params any) (json.RawMessage, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetReadLimit(2 << 20)
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetWriteDeadline(deadline)
	_ = conn.SetReadDeadline(deadline)
	if err = conn.WriteJSON(map[string]any{"id": 1, "method": method, "params": params}); err != nil {
		return nil, ErrUncertain
	}
	for {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err = conn.ReadJSON(&msg); err != nil {
			return nil, ErrUncertain
		}
		if msg.ID == 1 {
			if len(msg.Error) > 0 {
				return nil, errors.New("Browser input was rejected")
			}
			return msg.Result, nil
		}
	}
}

// pages lists Chrome's pages with the page that opened each (a popup names
// its opener), across every agent's windows.
func (h *linuxHost) pages(ctx context.Context) ([]tab, error) {
	raw, err := h.browserCDP(ctx, "Target.getTargets", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			OpenerID string `json:"openerId"`
		} `json:"targetInfos"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	pages := []tab{}
	for _, t := range out.TargetInfos {
		if t.Type == "page" {
			pages = append(pages, tab{ID: t.TargetID, Title: t.Title, URL: t.URL, Type: t.Type, Opener: t.OpenerID})
		}
	}
	return pages, nil
}

func (h *linuxHost) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	// Ask Chrome to flush its persistent profile before terminating helpers.
	// SIGTERM to the whole process group can kill the cookie writer too early.
	if h.chrome.alive() && h.endpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = h.browserCDP(ctx, "Browser.close", map[string]any{})
		_ = waitForExit(ctx, 4*time.Second, h.chrome)
		cancel()
	}
	h.chrome.stop()
	if h.pipeOut != nil {
		h.pipeOut.Close()
	}
	if h.pipeIn != nil {
		h.pipeIn.Close()
	}
	h.display.stop()
	if h.lock != nil {
		_ = unix.Flock(int(h.lock.Fd()), unix.LOCK_UN)
		_ = h.lock.Close()
	}
	if h.rundir != "" {
		_ = os.RemoveAll(h.rundir)
	}
	return nil
}

// waitForFile waits until ready reports true, re-checking it whenever dir
// changes, and gives up when d passes, ctx ends or the process that should
// create the file exits, so a crashed launch fails at once instead of after
// the timeout.
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

// linuxWindow is one agent's window as the person sees it: the page it shows
// now, input for that page and the live view of it.
type linuxWindow struct {
	h       *linuxHost
	mu      sync.Mutex
	target  string
	closed  bool
	keys    map[string]bool
	buttons map[string]bool
	x, y    float64
	streams map[*screencast]bool
}

func (r *linuxWindow) Target() string { r.mu.Lock(); defer r.mu.Unlock(); return r.target }
func (r *linuxWindow) Alive() bool {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	return !closed && r.h.Alive()
}

// SetTarget follows the window to another page; live views switch to it.
func (r *linuxWindow) SetTarget(target string) {
	r.mu.Lock()
	if r.target == target || target == "" {
		r.mu.Unlock()
		return
	}
	r.target = target
	streams := make([]*screencast, 0, len(r.streams))
	for s := range r.streams {
		streams = append(streams, s)
	}
	r.mu.Unlock()
	for _, s := range streams {
		s.retarget()
	}
}

func (r *linuxWindow) pageEndpoint(target string) string {
	return "ws://" + strings.TrimPrefix(r.h.endpoint, "http://") + "/devtools/page/" + target
}

func (r *linuxWindow) cdp(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return cdpCall(ctx, r.pageEndpoint(r.Target()), method, params)
}

// Page is the shown page's address without query or fragment.
func (r *linuxWindow) Page(ctx context.Context) string {
	pages, err := r.h.pages(ctx)
	if err != nil {
		return ""
	}
	target := r.Target()
	for _, p := range pages {
		if p.ID != target {
			continue
		}
		u, err := url.Parse(p.URL)
		if err != nil || u.Host == "" {
			return ""
		}
		return u.Scheme + "://" + u.Host + u.Path
	}
	return ""
}

type tab struct {
	ID     string
	Title  string
	URL    string
	Type   string
	Opener string
}

func validURL(value string) bool {
	if value == "about:blank" {
		return true
	}
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(value) <= 4096
}

// windowOf is the browser window a page is in.
func (h *linuxHost) windowOf(ctx context.Context, target string) (int, error) {
	raw, err := h.browserCDP(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": target})
	if err != nil {
		return 0, err
	}
	var out struct {
		WindowID int `json:"windowId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, err
	}
	return out.WindowID, nil
}

// tabs are the pages of this window.
func (r *linuxWindow) tabs(ctx context.Context) ([]tab, error) {
	pages, err := r.h.pages(ctx)
	if err != nil {
		return nil, err
	}
	window, err := r.h.windowOf(ctx, r.Target())
	if err != nil {
		return nil, err
	}
	out := []tab{}
	for _, p := range pages {
		if w, err := r.h.windowOf(ctx, p.ID); err == nil && w == window {
			out = append(out, p)
		}
	}
	return out, nil
}

// Command is the person's address bar and tab strip for the window.
func (r *linuxWindow) Command(ctx context.Context, c Command) (json.RawMessage, error) {
	if !r.Alive() {
		return nil, ErrUnavailable
	}
	switch c.Kind {
	case "navigate":
		if !validURL(c.URL) {
			return nil, errors.New("Enter an HTTP or HTTPS address")
		}
		raw, err := r.cdp(ctx, "Page.navigate", map[string]any{"url": c.URL})
		if err != nil {
			return nil, err
		}
		var started struct {
			ErrorText string `json:"errorText"`
		}
		_ = json.Unmarshal(raw, &started)
		if started.ErrorText != "" {
			return nil, fmt.Errorf("Could not open the page: %s", started.ErrorText)
		}
		return json.Marshal(map[string]any{"url": c.URL})
	case "tabs":
		tabs, err := r.tabs(ctx)
		if err != nil {
			return nil, err
		}
		safe := []map[string]string{}
		for _, t := range tabs {
			safe = append(safe, map[string]string{"id": t.ID, "title": t.Title, "url": t.URL})
		}
		return json.Marshal(safe)
	case "select_tab":
		tabs, err := r.tabs(ctx)
		if err != nil {
			return nil, err
		}
		for _, t := range tabs {
			if t.ID == c.Target {
				if _, err := r.h.browserCDP(ctx, "Target.activateTarget", map[string]any{"targetId": t.ID}); err != nil {
					return nil, err
				}
				r.SetTarget(t.ID)
				return json.RawMessage("{}"), nil
			}
		}
		return nil, errors.New("Tab not found")
	case "new_tab":
		if !validURL(c.URL) {
			return nil, errors.New("Enter an HTTP or HTTPS address")
		}
		// A page opens its own tab in its own window; the extension reports
		// the one that is now showing.
		expr := "window.open(" + strconv.Quote(c.URL) + ", '_blank') && true"
		if _, err := r.cdp(ctx, "Runtime.evaluate", map[string]any{"expression": expr, "userGesture": true}); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	default:
		return nil, errors.New("Unsupported browser command")
	}
}

var namedKeys = map[string]int{"Backspace": 8, "Tab": 9, "Enter": 13, "Escape": 27, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Delete": 46}

func (r *linuxWindow) Input(ctx context.Context, i Input) error {
	if !r.Alive() {
		return ErrUnavailable
	}
	if i.X < 0 || i.Y < 0 || i.X > 16384 || i.Y > 16384 || i.DX > 10000 || i.DX < -10000 || i.DY > 10000 || i.DY < -10000 || len(i.Text) > 8192 || len(i.Key) > 64 {
		return errors.New("Invalid input coordinates or text")
	}
	switch i.Kind {
	case "text":
		_, err := r.cdp(ctx, "Input.insertText", map[string]any{"text": i.Text})
		return err
	case "keyDown", "keyUp":
		r.mu.Lock()
		if i.Kind == "keyDown" {
			r.keys[i.Key] = true
		} else {
			delete(r.keys, i.Key)
		}
		r.mu.Unlock()
		params := map[string]any{"type": i.Kind, "key": i.Key, "text": i.Text}
		// Named keys need their key code for the page to act on them (Enter
		// submits, Tab moves on); typed characters arrive as text.
		if code, ok := namedKeys[i.Key]; ok {
			params["windowsVirtualKeyCode"] = code
			params["code"] = i.Key
			if i.Kind == "keyDown" && i.Key == "Enter" {
				params["text"] = "\r"
			}
		}
		_, err := r.cdp(ctx, "Input.dispatchKeyEvent", params)
		return err
	case "mouseMoved", "mousePressed", "mouseReleased", "mouseWheel":
		if i.Button != "" && i.Button != "left" && i.Button != "right" && i.Button != "middle" {
			return errors.New("Invalid pointer button")
		}
		r.mu.Lock()
		r.x = i.X
		r.y = i.Y
		if i.Kind == "mousePressed" {
			r.buttons[i.Button] = true
		} else if i.Kind == "mouseReleased" {
			delete(r.buttons, i.Button)
		}
		r.mu.Unlock()
		params := map[string]any{"type": i.Kind, "x": i.X, "y": i.Y, "button": i.Button, "clickCount": 1}
		if i.Button == "" {
			delete(params, "button")
		}
		if i.Kind == "mouseWheel" {
			params["deltaX"] = i.DX
			params["deltaY"] = i.DY
		}
		_, err := r.cdp(ctx, "Input.dispatchMouseEvent", params)
		return err
	default:
		return errors.New("Unsupported input event")
	}
}
func (r *linuxWindow) Release(ctx context.Context) error {
	r.mu.Lock()
	keys := r.keys
	buttons := r.buttons
	x, y := r.x, r.y
	r.keys = map[string]bool{}
	r.buttons = map[string]bool{}
	r.mu.Unlock()
	for k := range keys {
		if _, err := r.cdp(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k}); err != nil {
			return err
		}
	}
	for b := range buttons {
		if _, err := r.cdp(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": x, "y": y, "button": b}); err != nil {
			return err
		}
	}
	return nil
}

// Close ends the live views of this window. The window itself belongs to the
// extension, which closes it with the agent's connection.
func (r *linuxWindow) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	streams := make([]*screencast, 0, len(r.streams))
	for s := range r.streams {
		streams = append(streams, s)
	}
	r.mu.Unlock()
	for _, s := range streams {
		_ = s.Close()
	}
	return nil
}
