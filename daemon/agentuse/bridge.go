package agentuse

import (
	"bufio"
	"context"
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
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	RelayURL  string          `json:"relayUrl,omitempty"`
	Client    string          `json:"client,omitempty"`
	Bounds    *WindowBounds   `json:"bounds,omitempty"`
	Message   string          `json:"message,omitempty"`
	Reason    string          `json:"reason,omitempty"`
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
	Role     string        `json:"role"` // "host" or "connect"
	RelayURL string        `json:"relayUrl,omitempty"`
	Client   string        `json:"client,omitempty"`
	Bounds   *WindowBounds `json:"bounds,omitempty"`
}

type connectReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

var (
	// ErrNoExtensionHost means Chrome isn't running the Mewla extension now.
	ErrNoExtensionHost = errors.New("the Mewla extension isn't connected: Chrome isn't running with it")
	connectTimeout     = 20 * time.Second
)

func bridgeSocket(dir string) string { return filepath.Join(dir, "bridge.sock") }

// Bridge is the daemon's side.
type Bridge struct {
	dir string
	// Bounds, when set, says where the agent's window should go: the isolated
	// display, if the computer has one.
	Bounds func() (WindowBounds, bool)

	mu      sync.Mutex
	host    *bridgeHost
	waiting map[string]chan hostMessage
	nextID  int
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
	return &Bridge{dir: dir, waiting: map[string]chan hostMessage{}}
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
		b.serveHost(conn, r)
	case "connect":
		reply := connectReply{OK: true}
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		if err := b.Connect(ctx, h.RelayURL, h.Client, h.Bounds); err != nil {
			reply = connectReply{Error: err.Error()}
		}
		_ = json.NewEncoder(conn).Encode(reply)
	}
}

func (b *Bridge) serveHost(conn net.Conn, r *bufio.Reader) {
	host := &bridgeHost{conn: conn, enc: json.NewEncoder(conn)}
	b.mu.Lock()
	old := b.host
	b.host = host
	b.mu.Unlock()
	if old != nil {
		old.conn.Close() // a newer Chrome profile or window took over
	}
	defer func() {
		b.mu.Lock()
		if b.host == host {
			b.host = nil
		}
		b.mu.Unlock()
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
			b.mu.Lock()
			ch := b.waiting[m.ID]
			b.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		}
	}
}

func (b *Bridge) failAll(why string) {
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
	if err := validRelayURL(relayURL); err != nil {
		return err
	}
	b.mu.Lock()
	host := b.host
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
	if bounds == nil && b.Bounds != nil {
		if got, ok := b.Bounds(); ok {
			bounds = &got
		}
	}
	if err := host.send(hostMessage{Type: "connect", ID: id, RelayURL: relayURL, Client: client, Bounds: bounds}); err != nil {
		return fmt.Errorf("couldn't reach the extension: %w", err)
	}
	select {
	case m := <-ch:
		if m.Type == "connected" {
			return nil
		}
		return fmt.Errorf("the extension couldn't open the agent's window: %s", m.Message)
	case <-ctx.Done():
		_ = host.send(hostMessage{Type: "disconnect", ID: id})
		return errors.New("the extension didn't answer in time")
	}
}

// ConnectViaBridge is the connect role's side: ask the daemon.
func ConnectViaBridge(dir, relayURL, client string) error {
	conn, err := net.DialTimeout("unix", bridgeSocket(dir), 3*time.Second)
	if err != nil {
		return errors.New("Mewla isn't running on this computer")
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(hello{Role: "connect", RelayURL: relayURL, Client: client}); err != nil {
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
func DialBridgeHost(dir string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", bridgeSocket(dir), 3*time.Second)
	if err != nil {
		return nil, errors.New("Mewla isn't running on this computer")
	}
	if err := json.NewEncoder(conn).Encode(hello{Role: "host"}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
