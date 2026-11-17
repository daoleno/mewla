//go:build linux

package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/keeper"
	"github.com/daoleno/mewla/daemon/vdisplay"
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

// ownerEnv marks every process of the agent browser with the keeper that runs
// it, so a later daemon can tell what a crash left behind (reapStrays).
const ownerEnv = "MEWLA_BROWSER_OWNER"

// tempPrefix starts the private run directory of every runtime.
const tempPrefix = "zb-"

// linuxHost is the computer's one Chrome on the agent profile, running on the
// agent screen. The extension opens an agent's windows in it and places each
// on its agent's monitor; nothing here touches a window.
//
// Chrome runs in a keeper (package keeper), which holds its debugging pipe and
// relays it here, so a restart of Mewla keeps the browser and every agent's
// window in it: the next daemon attaches to the same keeper (Adopt).
type linuxHost struct {
	mu     sync.Mutex
	socket string
	pid    int
	closed bool
	pipe   *pipeCDP
}

// keepLinger is how long the agent browser outlives a daemon that does not
// come back; a restart attaches again well within it.
const keepLinger = 10 * time.Minute

// keeperSocket is where the agent browser's keeper listens: in the profile's
// directory, or a short private one when that path is too long for a socket.
func keeperSocket(root string) (string, error) {
	path := filepath.Join(root, "keeper.sock")
	if len(path) <= 100 {
		return path, nil
	}
	sum := sha256.Sum256([]byte(root))
	dir := filepath.Join("/tmp", fmt.Sprintf("mewla-%d", os.Getuid()))
	if err := privateDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, "b"+hex.EncodeToString(sum[:8])+".sock"), nil
}

// attachHost joins the keeper at socket and checks Chrome answers there.
func attachHost(ctx context.Context, socket string) (*linuxHost, error) {
	conn, err := keeper.Attach(socket)
	if err != nil {
		return nil, err
	}
	h := &linuxHost{socket: socket, pid: conn.Pid, pipe: newPipeCDP(conn, conn)}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := h.pipe.call(check, "Browser.getVersion", nil); err != nil {
		h.pipe.close()
		return nil, err
	}
	return h, nil
}

// Adopt is the agent browser a daemon before this one left running on the
// profile in root, or nil when there is none.
func (b *LinuxBackend) Adopt(ctx context.Context, root string) (Host, error) {
	socket, err := keeperSocket(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(socket); err != nil {
		return nil, nil
	}
	h, err := attachHost(ctx, socket)
	if err != nil {
		log.Printf("browser: the agent browser a daemon before this one left does not answer (%v); stopping it", err)
		_ = keeper.Stop(socket)
		return nil, nil
	}
	// It must be on the agent screen this daemon shows: one left on a screen
	// that was not taken back would put agents' windows where nobody sees them.
	if b.Screens == nil {
		h.Detach()
		return nil, nil
	}
	number, _, err := b.Screens.DisplayEnv(ctx)
	if err != nil {
		h.Detach()
		return nil, err
	}
	if on := displayOf(h.pid); on != fmt.Sprintf(":%d", number) {
		log.Printf("browser: the agent browser a daemon before this one left is on display %q, not the agent screen :%d; stopping it", on, number)
		h.Detach()
		_ = keeper.Stop(socket)
		return nil, nil
	}
	// Its extension reconnects through the native host registered in the
	// profile: that names this daemon's mewla, which an update may have moved.
	if b.Prepare != nil {
		if _, err := b.Prepare(filepath.Join(root, "profile")); err != nil {
			log.Printf("browser: couldn't register Mewla's native host again for the agent browser: %v", err)
		}
	}
	return h, nil
}

// displayOf is the X display a process runs on, from its environment.
func displayOf(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return ""
	}
	for _, kv := range strings.Split(string(raw), "\x00") {
		if v, ok := strings.CutPrefix(kv, "DISPLAY="); ok {
			return v
		}
	}
	return ""
}

func (b *LinuxBackend) Start(ctx context.Context, root string) (Host, error) {
	if c := b.Capability(ctx); !c.Available {
		return nil, errors.New(c.Reason)
	}
	if b.Screens == nil {
		return nil, errors.New("The agent screen is not available")
	}
	if err := privateDir(root); err != nil {
		return nil, err
	}
	// The browser a daemon before this one left, when it still answers.
	if h, err := b.Adopt(ctx, root); err != nil || h != nil {
		return h, err
	}
	reapStrays()
	profile := filepath.Join(root, "profile")
	if err := privateDir(profile); err != nil {
		return nil, err
	}
	socket, err := keeperSocket(root)
	if err != nil {
		return nil, err
	}
	store, err := credentialStore(ctx, root)
	if err != nil {
		return nil, err
	}
	number, xauth, err := b.Screens.DisplayEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("The agent screen could not start: %w", err)
	}
	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
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
	// ownerEnv marks every process of this runtime with its keeper (set by the
	// keeper), so a later daemon can tell what a crash left behind.
	env = append(env, "DBUS_SESSION_BUS_ADDRESS="+bus, "XDG_SESSION_TYPE=x11", "XDG_RUNTIME_DIR="+filepath.Join(dir, "run"), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), fmt.Sprintf("DISPLAY=:%d", number), "XAUTHORITY="+xauth)
	// The private profile lock is also enforced by Chrome. Never delete its SingletonLock.
	extension := ""
	if b.Prepare != nil {
		if extension, err = b.Prepare(profile); err != nil {
			return nil, fmt.Errorf("Mewla's extension could not be prepared: %w", err)
		}
	}
	logPath := filepath.Join(root, "keeper.log")
	_ = os.Remove(logPath)
	err = keeper.Spawn(keeper.Spec{
		Socket: socket, Linger: keepLinger, CDP: true, Env: env, OwnerEnv: ownerEnv,
		Lock: filepath.Join(root, "runtime.lock"), Log: logPath, Cleanup: []string{dir},
		Argv: chromeArgv(b.chrome(), profile, store),
	})
	if err != nil {
		if strings.Contains(err.Error(), "held by another runtime") {
			return nil, errors.New("The agent browser profile already has a runtime owner")
		}
		return nil, fmt.Errorf("Managed Chrome could not start: %w", err)
	}
	h, err := attachHost(ctx, socket)
	if err != nil {
		_ = keeper.Stop(socket)
		return nil, fmt.Errorf("Managed Chrome could not start: %v: %s", err, keeper.Tail(logPath))
	}
	defer func() {
		if !success {
			_ = h.Close()
		}
	}()
	if extension != "" {
		raw, err := h.pipe.call(ctx, "Extensions.loadUnpacked", map[string]any{"path": extension})
		if err != nil {
			return nil, fmt.Errorf("Mewla's extension could not be loaded into the agent browser: %v: %s", err, keeper.Tail(logPath))
		}
		var loaded struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &loaded)
		if loaded.ID == "" {
			return nil, errors.New("Mewla's extension could not be loaded into the agent browser: Chrome returned no extension ID")
		}
	}
	success = true
	return h, nil
}

// chromeArgv is how the agent browser's Chrome starts on the profile, keeping
// its sign-ins in store.
//
// Chrome 137 and later ignore --load-extension. An extension can still be
// loaded over the debugging pipe, which is also what lets nobody but this
// daemon do it: the pipe's other end is the keeper's, relayed only to the
// daemon over a socket only this user can open, and Chrome ends if it closes.
//
// The pipe also marks every page as automated (navigator.webdriver is true),
// and Google then refuses to sign in ("This browser or app may not be
// secure") at the first step, so the person could not give the agents their
// Google sign-in, or any site's "Sign in with Google". The browser is the
// person's and the agents', not a test harness: AutomationControlled is
// turned off. Attaching the debugger to a tab does not mark it.
//
// Despite Chrome's historical flag name, libsecret is the standard Secret
// Service client, also supported by installed KDE ksecretd/KWallet. The
// windows of different agents are on different monitors, but a window that
// is covered or off screen must not be slowed down for it.
func chromeArgv(chrome, profile, store string) []string {
	return []string{chrome, "--user-data-dir=" + profile, "--remote-debugging-pipe", "--enable-unsafe-extension-debugging", "--silent-debugger-extension-api", "--disable-blink-features=AutomationControlled", "--ozone-platform=x11", "--password-store=" + store, "--no-first-run", "--no-default-browser-check", "--no-startup-window", "--disable-features=OutdatedBuildDetector", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling"}
}

func (h *linuxHost) Alive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && !h.pipe.isEnded()
}

// Close stops the browser: its keeper asks Chrome to quit, so it flushes the
// persistent profile (the agents' sign-ins), then ends what is left.
func (h *linuxHost) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	err := keeper.Stop(h.socket)
	h.pipe.close()
	return err
}

// Detach lets go of the browser and leaves it running in its keeper, for the
// next daemon to adopt.
func (h *linuxHost) Detach() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.pipe.close()
}

// pipeCDP speaks to Chrome over its debugging pipe: NUL-delimited JSON, ours
// to read for as long as Chrome runs (it would block on a full pipe).
type pipeCDP struct {
	out     io.WriteCloser
	in      io.ReadCloser
	mu      sync.Mutex
	next    int
	pending map[int]chan pipeReply
	ended   bool
}

type pipeReply struct {
	result json.RawMessage
	err    error
}

func newPipeCDP(out io.WriteCloser, in io.ReadCloser) *pipeCDP {
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

func (p *pipeCDP) isEnded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

func (p *pipeCDP) close() {
	_ = p.out.Close()
	_ = p.in.Close()
}
