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
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

// LinuxBackend uses a private graphical session, not the user's seat or profile.
// Other GUI applications can use other runtime adapters without changing leases.
type LinuxBackend struct {
	Chrome string
}

func NewBackend() Backend { return &LinuxBackend{} }
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
	for _, name := range []string{"kwin_wayland", "dbus-run-session", "agent-browser"} {
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
	cmd := exec.Command(name, args...)
	cmd.Env = env
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
}

// linuxWindow is one agent's window: a page of its own, an agent-browser
// session pinned to it, and that session's live view. Sessions share the one
// Chrome, so each is pinned and every page it opens is a window of its own;
// pinned sessions do not follow one another's tabs.
type linuxWindow struct {
	h       *linuxHost
	session string
	mu      sync.Mutex
	target  string
	owned   map[string]bool // pages this window opened or was opened from
	closed  bool
	keys    map[string]bool
	buttons map[string]bool
	x, y    float64
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
	// Despite Chrome's historical flag name, libsecret is the standard Secret
	// Service client, also supported by installed KDE ksecretd/KWallet.
	// Windows of different agents overlap on one small screen; none of them may
	// be slowed down for being covered by another.
	r.chrome, err = spawn(env, b.chrome(), "--user-data-dir="+profile, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--ozone-platform=wayland", "--password-store="+store, "--no-first-run", "--no-default-browser-check", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling", "--window-size=1280,800", "about:blank")
	if err != nil {
		return nil, err
	}
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
	success = true
	return r, nil
}

// ownerEnv names the daemon pid in the environment of everything a runtime starts.
const ownerEnv = "MEWLA_BROWSER_OWNER"

// tempPrefix starts the private run directory of every runtime.
const tempPrefix = "zb-"

func (h *linuxHost) Alive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.chrome.alive() && h.display.alive()
}

func (h *linuxHost) sessionName() string {
	return "w" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
}

// Window opens a page of its own on the browser for one agent, with a session
// pinned to it.
func (h *linuxHost) Window(ctx context.Context) (Runtime, error) {
	if !h.Alive() {
		return nil, ErrUnavailable
	}
	w := &linuxWindow{h: h, session: h.sessionName(), owned: map[string]bool{}, keys: map[string]bool{}, buttons: map[string]bool{}}
	id, err := h.newPage(ctx, "about:blank")
	if err != nil {
		return nil, fmt.Errorf("Could not open a window: %w", err)
	}
	w.target = id
	w.owned[id] = true
	ok := false
	defer func() {
		if !ok {
			_ = w.Close()
		}
	}()
	if _, err = w.cli(ctx, "tab", id); err != nil {
		return nil, err
	}
	if err = w.configureViewport(ctx); err != nil {
		return nil, err
	}
	ok = true
	return w, nil
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

// newPage opens a page in a window of its own, so it is never hidden behind
// another agent's tabs.
func (h *linuxHost) newPage(ctx context.Context, url string) (string, error) {
	raw, err := h.browserCDP(ctx, "Target.createTarget", map[string]any{"url": url, "newWindow": true})
	if err != nil {
		return "", err
	}
	var out struct {
		TargetID string `json:"targetId"`
	}
	if json.Unmarshal(raw, &out) != nil || out.TargetID == "" {
		return "", errors.New("Chrome opened no page")
	}
	return out.TargetID, nil
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
	// Every session's agent-browser helper outlives the daemon unless told to go.
	if h.endpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = commandOutput(ctx, h.env, "agent-browser", "--json", "close", "--all")
		cancel()
	}
	// Ask Chrome to flush its persistent profile before terminating helpers.
	// SIGTERM to the whole process group can kill the cookie writer too early.
	if h.chrome.alive() && h.endpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = h.browserCDP(ctx, "Browser.close", map[string]any{})
		_ = waitForExit(ctx, 4*time.Second, h.chrome)
		cancel()
	}
	h.chrome.stop()
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
func (r *linuxWindow) Target() string { r.mu.Lock(); defer r.mu.Unlock(); return r.target }
func (r *linuxWindow) Alive() bool {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	return !closed && r.h.Alive()
}
func (r *linuxWindow) cli(ctx context.Context, args ...string) (json.RawMessage, error) {
	argv := append([]string{"--session", r.session, "--json", "--cdp", r.h.endpoint, "--pin-tab"}, args...)
	raw, err := commandOutput(ctx, r.h.env, "agent-browser", argv...)
	if ctx.Err() != nil {
		return nil, ErrUncertain
	}
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if len(raw) > 2<<20 || json.Unmarshal(raw, &response) != nil {
		return nil, errors.New("Browser tool returned an invalid response")
	}
	if err != nil || !response.Success {
		if msg := strings.TrimSpace(response.Error); msg != "" {
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return nil, fmt.Errorf("Browser action failed (%s); refresh the page state before retrying", msg)
		}
		return nil, errors.New("Browser action failed; refresh the page state before retrying")
	}
	return response.Data, nil
}

type tab struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Type   string `json:"type"`
	Socket string `json:"webSocketDebuggerUrl"`
	Opener string `json:"-"`
}

// tabs are this window's pages: the ones it opened and the popups they opened.
func (r *linuxWindow) tabs(ctx context.Context) ([]tab, error) {
	all, err := r.h.pages(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// A popup may name an opener that is itself a popup: settle until none is new.
	for grew := true; grew; {
		grew = false
		for _, t := range all {
			if !r.owned[t.ID] && t.Opener != "" && r.owned[t.Opener] {
				r.owned[t.ID] = true
				grew = true
			}
		}
	}
	out := []tab{}
	for _, t := range all {
		if r.owned[t.ID] {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *linuxWindow) cdp(ctx context.Context, method string, params any) (json.RawMessage, error) {
	tabs, err := r.tabs(ctx)
	if err != nil {
		return nil, err
	}
	target := r.Target()
	found := false
	for _, t := range tabs {
		found = found || t.ID == target
	}
	if !found {
		return nil, errors.New("Selected tab closed; choose another tab")
	}
	return cdpCall(ctx, "ws://"+strings.TrimPrefix(r.h.endpoint, "http://")+"/devtools/page/"+target, method, params)
}
func validURL(value string) bool {
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(value) <= 4096
}

// A CDP attachment starts with agent-browser's default viewport metadata, which
// can differ from the headed window's actual page size. Configure both through
// the maintained API so rendered pixels and pointer coordinates share a space.
// Newly selected popup pages need the same override as the original tab.
func (r *linuxWindow) configureViewport(ctx context.Context) error {
	_, err := r.cli(ctx, "set", "viewport", "1280", "800", "1")
	return err
}

func (r *linuxWindow) Command(ctx context.Context, c Command) (json.RawMessage, error) {
	if !r.Alive() {
		return nil, ErrUnavailable
	}
	if len(c.Text) > 8192 || len(c.Ref) > 256 {
		return nil, errors.New("Browser input exceeds its limit")
	}
	switch c.Kind {
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
		found := false
		for _, t := range tabs {
			if t.ID == c.Target {
				found = true
			}
		}
		if !found {
			return nil, errors.New("Tab not found")
		}
		if err = r.Release(ctx); err != nil {
			return nil, err
		}
		v, err := r.cli(ctx, "tab", c.Target)
		if err == nil {
			r.mu.Lock()
			r.target = c.Target
			r.mu.Unlock()
			if err = r.configureViewport(ctx); err != nil {
				return nil, ErrUncertain
			}
		}
		return v, err
	case "navigate", "new_tab":
		if !validURL(c.URL) {
			return nil, errors.New("Enter an HTTP or HTTPS address")
		}
		if c.Kind == "navigate" {
			return r.cli(ctx, "open", c.URL)
		}
		id, err := r.h.newPage(ctx, c.URL)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.owned[id] = true
		r.mu.Unlock()
		v, err := r.cli(ctx, "tab", id)
		if err == nil {
			r.mu.Lock()
			r.target = id
			r.mu.Unlock()
			if err = r.configureViewport(ctx); err != nil {
				return nil, ErrUncertain
			}
		}
		return v, err
	case "snapshot":
		return r.cli(ctx, "snapshot", "-i")
	case "click":
		ref, ok := elementRef(c.Ref)
		if !ok {
			return nil, errors.New("Use an element reference from the current snapshot")
		}
		return r.cli(ctx, "click", ref)
	case "fill":
		ref, ok := elementRef(c.Ref)
		if !ok {
			return nil, errors.New("Use an element reference from the current snapshot")
		}
		return r.cli(ctx, "fill", ref, c.Text)
	case "press":
		if len(c.Text) > 80 {
			return nil, errors.New("Key is too long")
		}
		return r.cli(ctx, "press", c.Text)
	case "dialog_status":
		return r.cli(ctx, "dialog", "status")
	case "dialog_accept":
		return r.cli(ctx, "dialog", "accept", c.Text)
	case "dialog_dismiss":
		return r.cli(ctx, "dialog", "dismiss")
	case "input":
		if c.Input == nil {
			return nil, errors.New("Input is required")
		}
		return r.input(ctx, *c.Input)
	default:
		return nil, errors.New("Unsupported managed browser action")
	}
}

// elementRef accepts a snapshot reference as the agent sees it, "e2" in the
// snapshot text or "@e2" as agent-browser takes it, and returns the latter.
func elementRef(ref string) (string, bool) {
	rest := strings.TrimPrefix(ref, "@")
	if !strings.HasPrefix(rest, "e") {
		return "", false
	}
	digits := rest[1:]
	if digits == "" || len(digits) > 9 || strings.Trim(digits, "0123456789") != "" {
		return "", false
	}
	return "@e" + digits, true
}

var namedKeys = map[string]int{"Backspace": 8, "Tab": 9, "Enter": 13, "Escape": 27, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Delete": 46}

func (r *linuxWindow) input(ctx context.Context, i Input) (json.RawMessage, error) {
	if i.X < 0 || i.Y < 0 || i.X > 16384 || i.Y > 16384 || i.DX > 10000 || i.DX < -10000 || i.DY > 10000 || i.DY < -10000 || len(i.Text) > 8192 || len(i.Key) > 64 {
		return nil, errors.New("Invalid input coordinates or text")
	}
	switch i.Kind {
	case "text":
		return r.cdp(ctx, "Input.insertText", map[string]any{"text": i.Text})
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
		return r.cdp(ctx, "Input.dispatchKeyEvent", params)
	case "mouseMoved", "mousePressed", "mouseReleased", "mouseWheel":
		if i.Button != "" && i.Button != "left" && i.Button != "right" && i.Button != "middle" {
			return nil, errors.New("Invalid pointer button")
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
		return r.cdp(ctx, "Input.dispatchMouseEvent", params)
	default:
		return nil, errors.New("Unsupported input event")
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
func (r *linuxWindow) Stream(ctx context.Context) (Stream, error) {
	raw, err := r.cli(ctx, "stream", "status")
	if err != nil {
		return nil, err
	}
	var status struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Port < 1 || status.Port > 65535 {
		return nil, errors.New("Browser viewer is unavailable")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, fmt.Sprintf("ws://127.0.0.1:%d/?pacing=ack&maxFps=12", status.Port), nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(3 << 20)
	return &agentStream{conn: conn}, nil
}

type agentStream struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *agentStream) Read() ([]byte, error) { _, raw, err := s.conn.ReadMessage(); return raw, err }
func (s *agentStream) Ack(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return s.conn.WriteJSON(map[string]any{"type": "ack", "seq": seq})
}
func (s *agentStream) Close() error {
	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "viewer detached"), time.Now().Add(time.Second))
	return s.conn.Close()
}

// Close shuts this window's pages and its session. The browser, the other
// agents' windows and the sign-ins stay.
func (r *linuxWindow) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	pages := make([]string, 0, len(r.owned))
	for id := range r.owned {
		pages = append(pages, id)
	}
	r.mu.Unlock()
	if r.h.Alive() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, _ = r.cli(ctx, "close")
		for _, id := range pages {
			_, _ = r.h.browserCDP(ctx, "Target.closeTarget", map[string]any{"targetId": id})
		}
	}
	return nil
}
