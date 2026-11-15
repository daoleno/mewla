package agentuse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// With the agent browser on, the bridge starts it before it asks, asks only
// the agent browser's extension (never the person's Chrome), puts the window
// on the agent screen, and tells the daemon the window, the page it shows and
// when it closes.
func TestAgentBrowserWindowsGoToTheAgentBrowserOnly(t *testing.T) {
	b, dir := startBridge(t)
	var mu sync.Mutex
	var events []string
	record := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, sprint(format, args...))
	}
	person := fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "connected", WindowID: 1} })
	waitHost(t, b)

	agentConn, err := DialBridgeHost(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer agentConn.Close()
	enc := json.NewEncoder(agentConn)
	asked := make(chan hostMessage, 4)
	go func() {
		dec := json.NewDecoder(agentConn)
		for {
			var m hostMessage
			if dec.Decode(&m) != nil {
				return
			}
			asked <- m
			if m.Type == "connect" {
				_ = enc.Encode(hostMessage{Type: "connected", ID: m.ID, WindowID: 7})
			}
		}
	}()
	ensured := 0
	b.Agent = &AgentBrowser{
		On:     func() bool { return true },
		Ensure: func(context.Context) error { ensured++; return nil },
		Monitor: func(_ context.Context, name string) (WindowBounds, error) {
			record("monitor %s", name)
			return WindowBounds{Left: 1280, Top: 0, Width: 1280, Height: 800}, nil
		},
		Opened: func(worker, key string) error {
			record("opened %s", worker)
			return nil
		},
		Closed: func(key string) { record("closed") },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.connect(ctx, goodRelay, "client", "worker-1", nil); err != nil {
		t.Fatal(err)
	}
	m := <-asked
	if m.Type != "connect" || m.Bounds == nil || *m.Bounds != (WindowBounds{1280, 0, 1280, 800}) {
		t.Fatalf("the agent browser was asked %+v", m)
	}
	if ensured != 1 {
		t.Fatalf("the agent browser was started %d times", ensured)
	}
	select {
	case m := <-person:
		if m.Type == "connect" {
			t.Fatal("the person's Chrome was asked to open an agent window")
		}
	case <-time.After(100 * time.Millisecond):
	}
	_ = enc.Encode(hostMessage{Type: "closed", ID: m.ID, Reason: "The agent finished"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"monitor worker-1", "opened worker-1", "closed"}
	if len(events) != 3 || events[0] != want[0] || events[1] != want[1] || events[2] != want[2] {
		t.Fatalf("events %v, want %v", events, want)
	}
}

// A browser that cannot start says why, and the person's Chrome is not tried instead.
func TestAgentBrowserStartFailureNamesItsCause(t *testing.T) {
	b, dir := startBridge(t)
	person := fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "connected"} })
	waitHost(t, b)
	b.Agent = &AgentBrowser{On: func() bool { return true }, Ensure: func(context.Context) error { return errors.New("the agent screen could not start") }}
	err := b.connect(context.Background(), goodRelay, "client", "w", nil)
	if err == nil || !contains(err.Error(), "the agent screen could not start") {
		t.Fatalf("err = %v", err)
	}
	select {
	case m := <-person:
		if m.Type == "connect" {
			t.Fatal("the person's Chrome was tried")
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func sprint(format string, args ...any) string { return fmt.Sprintf(format, args...) }
func contains(s, sub string) bool              { return strings.Contains(s, sub) }

// The agent browser keeps its windows while Mewla restarts. The new daemon's
// bridge tells its extension it is the agent browser's, lists again the windows
// its hello names (under their Workers), and gives new windows ids that cannot
// be one of those. A window a reconnected host does not name again goes off
// the list after the grace; one it names stays.
func TestTheAgentBrowsersWindowsAreListedAgainAfterARestart(t *testing.T) {
	first := NewBridge(t.TempDir())
	b, dir := startBridge(t)
	b.mu.Lock()
	b.hostGrace = 200 * time.Millisecond
	b.mu.Unlock()
	if first.run == b.run {
		t.Fatal("two daemons' bridges name their windows alike")
	}
	var mu sync.Mutex
	opened := map[string]string{}
	var closed []string
	b.Agent = &AgentBrowser{
		On: func() bool { return true },
		Monitor: func(context.Context, string) (WindowBounds, error) {
			return WindowBounds{Width: 1280, Height: 800}, nil
		},
		Opened: func(worker, key string) error {
			mu.Lock()
			defer mu.Unlock()
			opened[key] = worker
			return nil
		},
		Closed: func(key string) {
			mu.Lock()
			defer mu.Unlock()
			closed = append(closed, key)
		},
	}
	join := func(windows ...openWindow) (*json.Encoder, <-chan hostMessage, func()) {
		t.Helper()
		conn, err := DialBridgeHost(dir, true)
		if err != nil {
			t.Fatal(err)
		}
		enc := json.NewEncoder(conn)
		got := make(chan hostMessage, 8)
		go func() {
			dec := json.NewDecoder(conn)
			for {
				var m hostMessage
				if dec.Decode(&m) != nil {
					return
				}
				got <- m
				if m.Type == "connect" {
					_ = enc.Encode(hostMessage{Type: "connected", ID: m.ID})
				}
			}
		}()
		_ = enc.Encode(hostMessage{Type: "hello", Version: "9.9", Windows: windows})
		return enc, got, func() { conn.Close() }
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			done := ok()
			mu.Unlock()
			if done {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal(what)
	}
	_, got, leave := join(openWindow{ID: "cold-1", Worker: "mh-a"}, openWindow{ID: "cold-2", Worker: "mh-b"})
	if m := <-got; m.Type != "role" || !m.Agent {
		t.Fatalf("the agent browser's extension was told %+v", m)
	}
	wait("the kept windows were not listed again", func() bool { return opened["cold-1"] == "mh-a" && opened["cold-2"] == "mh-b" })
	// Its host restarts; the extension comes back with one of the two.
	leave()
	_, got, leave = join(openWindow{ID: "cold-1", Worker: "mh-a"})
	defer leave()
	<-got
	wait("a window the extension no longer has stayed listed", func() bool { return len(closed) == 1 && closed[0] == "cold-2" })
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if len(closed) != 1 {
		t.Fatalf("closed %v: a window the extension named again went off the list", closed)
	}
	mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.connect(ctx, goodRelay, "client", "mh-c", nil); err != nil {
		t.Fatal(err)
	}
	m := <-got
	if m.Type != "connect" || m.Worker != "mh-c" || !strings.HasPrefix(m.ID, "c"+b.run+"-") {
		t.Fatalf("a new window was asked as %+v", m)
	}
}
