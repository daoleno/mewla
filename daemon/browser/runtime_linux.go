//go:build linux

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

// LinuxBackend uses a private graphical session, not the user's seat or profile.
// Other GUI applications can use other runtime adapters without changing leases.
type LinuxBackend struct {
	// Only package tests can bypass the real Secret Service requirement.
	fixtureBasicStore bool
	Chrome            string
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
	for _, name := range []string{"kwin_wayland", "dbus-run-session", "agent-browser", "gdbus"} {
		if _, e := exec.LookPath(name); e != nil {
			return Capability{Reason: name + " is not installed on this server"}
		}
	}
	if b.fixtureBasicStore {
		return Capability{Available: true}
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// NameHasOwner does not activate or unlock a personal keyring.
	raw, err := commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus", "--method", "org.freedesktop.DBus.NameHasOwner", "org.freedesktop.secrets")
	if err != nil || !strings.Contains(string(raw), "true") {
		return Capability{Reason: "A secure wallet is not running. Start and unlock KWallet or another Secret Service for the server user, then refresh Browser"}
	}
	raw, err = commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", "/org/freedesktop/secrets", "--method", "org.freedesktop.Secret.Service.ReadAlias", "default")
	if err != nil {
		return Capability{Reason: "The server's secure keyring is unavailable"}
	}
	// gdbus's object-path result is a typed D-Bus tuple, not a shell command.
	parts := strings.Split(string(raw), "'")
	if len(parts) < 3 || !strings.HasPrefix(parts[1], "/org/freedesktop/secrets/collection/") {
		return Capability{Reason: "Create a default collection in the server user's secure keyring"}
	}
	raw, err = commandOutput(check, nil, "gdbus", "call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", parts[1], "--method", "org.freedesktop.DBus.Properties.Get", "org.freedesktop.Secret.Collection", "Locked")
	if err != nil || !strings.Contains(string(raw), "false") {
		return Capability{Reason: "Unlock the server user's default wallet through its normal KWallet/Secret Service setup, then refresh Browser"}
	}
	return Capability{Available: true}
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func spawn(env []string, name string, args ...string) (*child, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &child{cmd: cmd, done: make(chan struct{})}
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

type linuxRuntime struct {
	mu                                         sync.Mutex
	profile, rundir, endpoint, session, target string
	env                                        []string
	chrome, display                            *child
	lock                                       *os.File
	closed                                     bool
	keys                                       map[string]bool
	buttons                                    map[string]bool
	x, y                                       float64
}

func (b *LinuxBackend) Start(ctx context.Context, root string) (Runtime, error) {
	if c := b.Capability(ctx); !c.Available {
		return nil, errors.New(c.Reason)
	}
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
		return nil, errors.New("This browser profile already has a runtime owner")
	}
	dir, err := os.MkdirTemp("", "zb-")
	if err != nil {
		lock.Close()
		return nil, err
	}
	r := &linuxRuntime{profile: profile, rundir: dir, lock: lock, session: "b" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8], keys: map[string]bool{}, buttons: map[string]bool{}}
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
		if k == "DBUS_SESSION_BUS_ADDRESS" || k == "DISPLAY" || k == "WAYLAND_DISPLAY" || k == "SESSION_MANAGER" || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "AGENT_BROWSER_") {
			continue
		}
		env = append(env, v)
	}
	// Chrome retains the daemon user's stable Secret Service bus. KWin gets its
	// own bus through dbus-run-session and never joins the user's physical seat.
	env = append(env, "DBUS_SESSION_BUS_ADDRESS="+bus, "XDG_SESSION_TYPE=wayland", "XDG_RUNTIME_DIR="+filepath.Join(dir, "run"), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), "WAYLAND_DISPLAY=zen-browser", "AGENT_BROWSER_SOCKET_DIR="+filepath.Join(dir, "s"), "AGENT_BROWSER_SESSION="+r.session, "AGENT_BROWSER_IDLE_TIMEOUT_MS=0", "AGENT_BROWSER_STREAM_QUALITY=65", "AGENT_BROWSER_STREAM_MAX_WIDTH=1280", "AGENT_BROWSER_STREAM_MAX_HEIGHT=800", "AGENT_BROWSER_NO_AUTO_DIALOG=1")
	r.env = env
	r.display, err = spawn(env, "dbus-run-session", "--", "kwin_wayland", "--virtual", "--width", "1280", "--height", "800", "--socket", "zen-browser", "--no-lockscreen", "--no-global-shortcuts", "--no-kactivities")
	if err != nil {
		return nil, err
	}
	if err = waitFor(ctx, 10*time.Second, func() bool { _, e := os.Stat(filepath.Join(dir, "run", "zen-browser")); return e == nil }); err != nil {
		return nil, errors.New("Private graphical session could not start")
	}
	// The private profile lock is also enforced by Chrome. Never delete its SingletonLock.
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	// Despite Chrome's historical flag name, libsecret is the standard Secret
	// Service client, also supported by installed KDE ksecretd/KWallet.
	store := "gnome-libsecret"
	if b.fixtureBasicStore {
		store = "basic"
	}
	r.chrome, err = spawn(env, b.chrome(), "--user-data-dir="+profile, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--ozone-platform=wayland", "--password-store="+store, "--no-first-run", "--no-default-browser-check", "--window-size=1280,800", "about:blank")
	if err != nil {
		return nil, err
	}
	if err = waitFor(ctx, 10*time.Second, func() bool {
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
		return nil, errors.New("Managed Chrome could not start; check its profile lock and graphical runtime")
	}
	tabs, err := r.tabs(ctx)
	if err != nil || len(tabs) == 0 {
		return nil, errors.New("Managed browser has no page")
	}
	r.target = tabs[0].ID
	if _, err = r.cli(ctx, "tab", r.target); err != nil {
		return nil, err
	}
	if _, err = r.cli(ctx, "--pin-tab", "tab", "list"); err != nil {
		return nil, err
	}
	success = true
	return r, nil
}
func waitFor(ctx context.Context, d time.Duration, f func() bool) error {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		if f() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return context.DeadlineExceeded
		case <-t.C:
		}
	}
}
func (r *linuxRuntime) Target() string { r.mu.Lock(); defer r.mu.Unlock(); return r.target }
func (r *linuxRuntime) Alive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.chrome.alive() && r.display.alive()
}
func (r *linuxRuntime) cli(ctx context.Context, args ...string) (json.RawMessage, error) {
	argv := append([]string{"--session", r.session, "--json", "--cdp", r.endpoint}, args...)
	raw, err := commandOutput(ctx, r.env, "agent-browser", argv...)
	if ctx.Err() != nil {
		return nil, ErrUncertain
	}
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if len(raw) > 2<<20 || json.Unmarshal(raw, &response) != nil {
		return nil, errors.New("Browser tool returned an invalid response")
	}
	if err != nil || !response.Success {
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
}

func (r *linuxRuntime) tabs(ctx context.Context) ([]tab, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", r.endpoint+"/json/list", nil)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	defer client.CloseIdleConnections()
	var all []tab
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&all); err != nil {
		return nil, err
	}
	out := []tab{}
	for _, t := range all {
		if t.Type == "page" {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *linuxRuntime) cdp(ctx context.Context, method string, params any) (json.RawMessage, error) {
	tabs, err := r.tabs(ctx)
	if err != nil {
		return nil, err
	}
	target := r.Target()
	endpoint := ""
	for _, t := range tabs {
		if t.ID == target {
			endpoint = t.Socket
		}
	}
	if endpoint == "" {
		return nil, errors.New("Selected tab closed; choose another tab")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "127.0.0.1" {
		return nil, errors.New("Invalid managed browser endpoint")
	}
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
func validURL(value string) bool {
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(value) <= 4096
}
func (r *linuxRuntime) Command(ctx context.Context, c Command) (json.RawMessage, error) {
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
		}
		return v, err
	case "navigate", "new_tab":
		if !validURL(c.URL) {
			return nil, errors.New("Enter an HTTP or HTTPS address")
		}
		if c.Kind == "navigate" {
			return r.cli(ctx, "open", c.URL)
		}
		v, err := r.cli(ctx, "tab", "new", c.URL)
		if err == nil {
			var q struct {
				TargetID string `json:"targetId"`
			}
			_ = json.Unmarshal(v, &q)
			if q.TargetID != "" {
				r.mu.Lock()
				r.target = q.TargetID
				r.mu.Unlock()
			}
		}
		return v, err
	case "snapshot":
		return r.cli(ctx, "snapshot", "-i")
	case "click":
		if !strings.HasPrefix(c.Ref, "@e") {
			return nil, errors.New("Use an element reference from the current snapshot")
		}
		return r.cli(ctx, "click", c.Ref)
	case "fill":
		if !strings.HasPrefix(c.Ref, "@e") {
			return nil, errors.New("Use an element reference from the current snapshot")
		}
		return r.cli(ctx, "fill", c.Ref, c.Text)
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
func (r *linuxRuntime) input(ctx context.Context, i Input) (json.RawMessage, error) {
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
		return r.cdp(ctx, "Input.dispatchKeyEvent", map[string]any{"type": i.Kind, "key": i.Key, "text": i.Text})
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
func (r *linuxRuntime) Release(ctx context.Context) error {
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
func (r *linuxRuntime) Stream(ctx context.Context) (Stream, error) {
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
func (r *linuxRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	if r.endpoint != "" {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = r.cli(ctx, "close")
		c()
	}
	// Ask Chrome to flush its persistent profile before terminating helpers.
	// SIGTERM to the whole process group can kill the cookie writer too early.
	if r.chrome.alive() && r.endpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = r.cdp(ctx, "Browser.close", map[string]any{})
		_ = waitFor(ctx, 4*time.Second, func() bool { return !r.chrome.alive() })
		cancel()
	}
	r.chrome.stop()
	r.display.stop()
	if r.lock != nil {
		_ = unix.Flock(int(r.lock.Fd()), unix.LOCK_UN)
		_ = r.lock.Close()
	}
	if r.rundir != "" {
		_ = os.RemoveAll(r.rundir)
	}
	return nil
}
