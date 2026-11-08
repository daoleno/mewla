package agentuse

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The bridge joins three programs on this computer, all the same user's:
//
//   - the Mewla extension in Chrome, reached through Chrome's native
//     messaging by `mewla agent-use native-host` (the host role);
//   - the browser tools an agent runs, which ask Chrome to connect by starting
//     the "browser" Mewla puts in their place, `mewla agent-use open` (the
//     connect role);
//   - the daemon, which holds the socket and passes one to the other.
//
// There is no key: Chrome starts the native host only for the extension ID in
// the host manifest, and the socket is the user's alone.

// Messages between the daemon and the extension, as the extension sends them.
type hostMessage struct {
	Type     string        `json:"type"`
	ID       string        `json:"id,omitempty"`
	RelayURL string        `json:"relayUrl,omitempty"`
	Client   string        `json:"client,omitempty"`
	Bounds   *WindowBounds `json:"bounds,omitempty"`
	Message  string        `json:"message,omitempty"`
	Reason   string        `json:"reason,omitempty"`
	// Label names the agent's tab group.
	Label     string          `json:"label,omitempty"`
	Version   string          `json:"version,omitempty"`
	WindowID  int             `json:"windowId,omitempty"`
	Sessions  []string        `json:"sessions,omitempty"`
	Extension json.RawMessage `json:"-"`
}

// WindowBounds is where the agent's window goes, in the global points of the
// computer's display space.
type WindowBounds struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// hello is the first line of every connection to the bridge.
type hello struct {
	Role string `json:"role"` // "host" or "connect"
	// Agent is set by the host of the agent browser (the virtual screen's
	// Chrome), which the person's Chrome never is.
	Agent    bool          `json:"agent,omitempty"`
	RelayURL string        `json:"relayUrl,omitempty"`
	Client   string        `json:"client,omitempty"`
	Bounds   *WindowBounds `json:"bounds,omitempty"`
	// Worker is the Mewla Worker the browser tools run for (MEWLA_WORKER_ID):
	// its name is the label of the tab group its tabs are in.
	Worker string `json:"worker,omitempty"`
}

type connectReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

var (
	// ErrNoExtensionHost means Chrome isn't running the Mewla extension now.
	ErrNoExtensionHost = errors.New("the Mewla extension isn't connected: Chrome isn't running with it")
	connectTimeout     = 60 * time.Second
)

// maxSocketPath keeps the socket's path inside what a unix socket can name:
// 104 bytes on a Mac, 108 on Linux, the terminator included.
const maxSocketPath = 100

// bridgeSocket is where the daemon listens and the host and the shim dial. A
// state directory deep enough to overflow a unix socket's path (a temporary
// directory under a long name) gets a short private directory of its own under
// /tmp (not $TMPDIR, which may be the long one), the
// same one on both sides because it derives from the state directory.
func bridgeSocket(dir string) string {
	path := filepath.Join(dir, "bridge.sock")
	if len(path) <= maxSocketPath {
		return path
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join("/tmp", fmt.Sprintf("mewla-%d", os.Getuid()), hex.EncodeToString(sum[:8])+".sock")
}

// privateDir makes the directory for a socket outside the state directory and
// makes sure it is the caller's own, not somebody else's or a link to one.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return os.Chmod(dir, 0o700)
}

// Bridge is the daemon's side.
type Bridge struct {
	dir string
	// Display, when set, makes the agent display (the display Mewla keeps for
	// agents, off the person's screen) and says where it is, for the agent's
	// window to open on; release lets it go when the window closes.
	Display func(key string) (bounds WindowBounds, release func(), err error)
	// WorkerName gives a Worker's name for its tab group, when it has one.
	WorkerName func(worker string) string
	// Agent, when set and on, puts agents' windows in the agent browser on the
	// virtual screen instead of the person's Chrome.
	Agent *AgentBrowser

	mu        sync.Mutex
	host      *bridgeHost
	agentHost *bridgeHost
	waiting   map[string]chan hostMessage
	// open are the agent browser's windows, by connection id, with the Worker.
	open   map[string]string
	nextID int
	// releases let go of each open agent window's hold on the display.
	releases map[string]func()
	// slots are the places on the agent display the open windows have, so
	// Workers' windows sit side by side, not over one another (a covered
	// window is one Chrome may stop drawing).
	slots map[string]int
}

// AgentBrowser is the agent browser on the virtual screen, as the bridge
// reaches it. The daemon's browser manager is on the other side of these.
type AgentBrowser struct {
	// On says whether agents' windows go there now.
	On func() bool
	// Ensure starts the agent browser and waits until it is up.
	Ensure func(ctx context.Context) error
	// Monitor is where the window of a Worker (or, with no Worker, of the
	// connection named "window:" and its id) goes on the agent screen: the
	// Worker's monitor, given to it if it has none.
	Monitor func(ctx context.Context, name string) (WindowBounds, error)
	// Opened lists a window the extension opened for a Worker (key is the
	// connection id); Closed takes it off the list.
	Opened func(worker, key string) error
	Closed func(key string)
}

type bridgeHost struct {
	conn    net.Conn
	enc     *json.Encoder
	mu      sync.Mutex
	version string
}

func (h *bridgeHost) send(m hostMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enc.Encode(m)
}

func NewBridge(dir string) *Bridge {
	return &Bridge{dir: dir, waiting: map[string]chan hostMessage{}, releases: map[string]func(){}, open: map[string]string{}}
}

// CloseWindow asks the extension to close an agent window by its connection id.
func (b *Bridge) CloseWindow(id string) {
	b.mu.Lock()
	host := b.agentHost
	if host == nil {
		host = b.host
	}
	b.mu.Unlock()
	if host != nil {
		_ = host.send(hostMessage{Type: "disconnect", ID: id})
	}
}

// HasAgentHost says whether the agent browser's extension is connected.
func (b *Bridge) HasAgentHost() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agentHost != nil
}

// HasHost says whether the extension is connected.
func (b *Bridge) HasHost() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.host != nil
}

// HostVersion is the connected extension's version, or "".
func (b *Bridge) HostVersion() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.host == nil {
		return ""
	}
	return b.host.version
}

// Serve listens until ctx ends.
func (b *Bridge) Serve(ctx context.Context) error {
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return err
	}
	path := bridgeSocket(b.dir)
	if filepath.Dir(path) != b.dir {
		if err := privateDir(filepath.Dir(path)); err != nil {
			return err
		}
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		ln.Close()
		_ = os.Remove(path)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go b.serve(conn)
	}
}

func (b *Bridge) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	var h hello
	if json.Unmarshal(line, &h) != nil {
		return
	}
	switch h.Role {
	case "host":
		b.serveHost(conn, r, h.Agent)
	case "connect":
		reply := connectReply{OK: true}
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		if err := b.connect(ctx, h.RelayURL, h.Client, h.Worker, h.Bounds); err != nil {
			reply = connectReply{Error: err.Error()}
		}
		_ = json.NewEncoder(conn).Encode(reply)
	}
}

func (b *Bridge) serveHost(conn net.Conn, r *bufio.Reader, agent bool) {
	host := &bridgeHost{conn: conn, enc: json.NewEncoder(conn)}
	b.mu.Lock()
	slot := &b.host
	if agent {
		slot = &b.agentHost
	}
	old := *slot
	*slot = host
	b.mu.Unlock()
	if old != nil {
		old.conn.Close() // a newer Chrome profile or window took over
	}
	defer func() {
		b.mu.Lock()
		if *slot == host {
			*slot = nil
		}
		var gone []string
		if agent {
			for id := range b.open {
				gone = append(gone, id)
			}
			b.open = map[string]string{}
		}
		b.mu.Unlock()
		if agent && b.Agent != nil && b.Agent.Closed != nil {
			for _, id := range gone {
				b.Agent.Closed(id)
			}
		}
		b.failAll("Chrome closed the Mewla extension")
	}()
	dec := json.NewDecoder(r)
	for {
		var m hostMessage
		if err := dec.Decode(&m); err != nil {
			return
		}
		switch m.Type {
		case "hello", "pong":
			b.mu.Lock()
			host.version = m.Version
			b.mu.Unlock()
		case "connected", "error", "closed":
			if m.Type != "connected" {
				b.release(m.ID)
			}
			if agent && m.Type != "connected" {
				b.mu.Lock()
				_, was := b.open[m.ID]
				delete(b.open, m.ID)
				b.mu.Unlock()
				if was && b.Agent != nil && b.Agent.Closed != nil {
					b.Agent.Closed(m.ID)
				}
			}
			b.mu.Lock()
			ch := b.waiting[m.ID]
			b.mu.Unlock()
			if ch != nil {
				if m.Type == "closed" && m.Message == "" {
					m.Message = m.Reason // a window that closed while opening says why here
				}
				select {
				case ch <- m:
				default:
				}
			}
		}
	}
}

// release lets go of a window's hold on the agent display.
func (b *Bridge) release(id string) {
	b.mu.Lock()
	release := b.releases[id]
	delete(b.releases, id)
	delete(b.slots, id)
	b.mu.Unlock()
	if release != nil {
		release()
	}
}

func (b *Bridge) failAll(why string) {
	b.mu.Lock()
	ids := make([]string, 0, len(b.releases))
	for id := range b.releases {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, id := range ids {
		b.release(id)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.waiting {
		select {
		case ch <- hostMessage{Type: "error", ID: id, Message: why}:
		default:
		}
	}
}

// validRelayURL accepts only the browser tools' own relay on this computer.
func validRelayURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ws" {
		return errors.New("not a local relay address")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("the relay must be on this computer")
	}
	if _, err := strconv.Atoi(u.Port()); err != nil || !strings.HasPrefix(u.Path, "/extension/") {
		return errors.New("not the browser tools' relay address")
	}
	return nil
}

// Connect asks the extension to open an agent window and connect it to the
// browser tools' relay, and returns when it has, or says why it couldn't.
func (b *Bridge) Connect(ctx context.Context, relayURL, client string, bounds *WindowBounds) error {
	return b.connect(ctx, relayURL, client, "", bounds)
}

func (b *Bridge) connect(ctx context.Context, relayURL, client, worker string, bounds *WindowBounds) error {
	if err := validRelayURL(relayURL); err != nil {
		return err
	}
	agent := b.Agent != nil && b.Agent.On != nil && b.Agent.On()
	if agent {
		// The agent browser starts when the first agent asks for it.
		if b.Agent.Ensure != nil {
			if err := b.Agent.Ensure(ctx); err != nil {
				return fmt.Errorf("the agent browser could not start: %w", err)
			}
		}
		for !b.HasAgentHost() {
			select {
			case <-ctx.Done():
				return errors.New("the agent browser started but its Mewla extension did not connect")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	b.mu.Lock()
	host := b.host
	if agent {
		host = b.agentHost
	}
	if host == nil {
		b.mu.Unlock()
		return ErrNoExtensionHost
	}
	b.nextID++
	id := fmt.Sprintf("c%d", b.nextID)
	ch := make(chan hostMessage, 1)
	b.waiting[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
	}()
	if agent {
		// The window fills the Worker's monitor on the agent screen.
		name := worker
		if name == "" {
			name = "window:" + id
		}
		if b.Agent.Monitor == nil {
			return errors.New("the agent screen has no monitor for this window")
		}
		cell, err := b.Agent.Monitor(ctx, name)
		if err != nil {
			return fmt.Errorf("the agent screen has no monitor for this window: %w", err)
		}
		bounds = &cell
	} else if bounds == nil && b.Display != nil {
		if got, release, err := b.Display(id); err == nil {
			b.mu.Lock()
			b.releases[id] = release
			cell := cellOf(got, b.takeSlotLocked(id))
			b.mu.Unlock()
			bounds = &cell
		}
	}
	label := ""
	if worker != "" {
		label = worker
		if b.WorkerName != nil {
			if name := strings.TrimSpace(b.WorkerName(worker)); name != "" {
				label = name
			}
		}
	}
	if err := host.send(hostMessage{Type: "connect", ID: id, RelayURL: relayURL, Client: client, Label: label, Bounds: bounds}); err != nil {
		b.release(id)
		return fmt.Errorf("couldn't reach the extension: %w", err)
	}
	select {
	case m := <-ch:
		if m.Type != "connected" {
			b.release(id)
			return fmt.Errorf("the extension couldn't open the agent's window: %s", m.Message)
		}
		if agent {
			b.mu.Lock()
			b.open[id] = worker
			b.mu.Unlock()
			if b.Agent.Opened != nil {
				if err := b.Agent.Opened(worker, id); err != nil {
					b.mu.Lock()
					delete(b.open, id)
					b.mu.Unlock()
					_ = host.send(hostMessage{Type: "disconnect", ID: id})
					return fmt.Errorf("the agent's window could not be shown in Screen: %w", err)
				}
			}
		}
		return nil
	case <-ctx.Done():
		_ = host.send(hostMessage{Type: "disconnect", ID: id})
		b.release(id)
		return errors.New("the extension didn't answer in time")
	}
}

// ConnectViaBridge is the connect role's side: ask the daemon.
func ConnectViaBridge(dir, relayURL, client string) error {
	return ConnectViaBridgeFor(dir, relayURL, client, "")
}

// ConnectViaBridgeFor is ConnectViaBridge for a Worker, whose name labels the
// tab group its tabs are in.
func ConnectViaBridgeFor(dir, relayURL, client, worker string) error {
	conn, err := net.DialTimeout("unix", bridgeSocket(dir), 3*time.Second)
	if err != nil {
		return errors.New("Mewla isn't running on this computer")
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(hello{Role: "connect", RelayURL: relayURL, Client: client, Worker: worker}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(connectTimeout + 5*time.Second))
	var reply connectReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return fmt.Errorf("Mewla didn't answer: %w", err)
	}
	if !reply.OK {
		return errors.New(reply.Error)
	}
	return nil
}

// DialBridgeHost is the host role's side: a connection to the daemon, already
// introduced as the extension's host.
func DialBridgeHost(dir string, agent bool) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", bridgeSocket(dir), 3*time.Second)
	if err != nil {
		return nil, errors.New("Mewla isn't running on this computer")
	}
	if err := json.NewEncoder(conn).Encode(hello{Role: "host", Agent: agent}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// takeSlotLocked is the lowest place on the display no open window has.
func (b *Bridge) takeSlotLocked(id string) int {
	if b.slots == nil {
		b.slots = map[string]int{}
	}
	taken := map[int]bool{}
	for _, slot := range b.slots {
		taken[slot] = true
	}
	slot := 0
	for taken[slot] {
		slot++
	}
	b.slots[id] = slot
	return slot
}

// cellOf is one quarter of the agent display: where the slot's window goes.
func cellOf(display WindowBounds, slot int) WindowBounds {
	w, h := display.Width/2, display.Height/2
	i := slot % 4
	return WindowBounds{Left: display.Left + (i%2)*w, Top: display.Top + (i/2)*h, Width: w, Height: h}
}
