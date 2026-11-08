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
				_ = enc.Encode(hostMessage{Type: "connected", ID: m.ID, WindowID: 7, TargetID: "page-1"})
			}
		}
	}()
	ensured := 0
	b.Agent = &AgentBrowser{
		On:     func() bool { return true },
		Ensure: func(context.Context) error { ensured++; return nil },
		Opened: func(_ context.Context, worker, key, target string) error {
			record("opened %s %s", worker, target)
			return nil
		},
		Viewed: func(key, target string) { record("viewed %s", target) },
		Closed: func(key string) { record("closed") },
		Bounds: WindowBounds{Left: 0, Top: 0, Width: 1280, Height: 800},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.connect(ctx, goodRelay, "client", "worker-1", nil); err != nil {
		t.Fatal(err)
	}
	m := <-asked
	if m.Type != "connect" || m.Bounds == nil || *m.Bounds != (WindowBounds{0, 0, 1280, 800}) {
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
	_ = enc.Encode(hostMessage{Type: "view", ID: m.ID, TargetID: "page-2"})
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
	want := []string{"opened worker-1 page-1", "viewed page-2", "closed"}
	if len(events) != 3 || events[0] != want[0] || events[1] != want[1] || events[2] != want[2] {
		t.Fatalf("events %v, want %v", events, want)
	}
}

// A browser that cannot start says why, and the person's Chrome is not tried instead.
func TestAgentBrowserStartFailureNamesItsCause(t *testing.T) {
	b, dir := startBridge(t)
	person := fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "connected"} })
	waitHost(t, b)
	b.Agent = &AgentBrowser{On: func() bool { return true }, Ensure: func(context.Context) error { return errors.New("kwin_wayland could not start") }}
	err := b.connect(context.Background(), goodRelay, "client", "w", nil)
	if err == nil || !contains(err.Error(), "kwin_wayland could not start") {
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
