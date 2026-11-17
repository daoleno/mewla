package agentuse

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Reflow asks the extension to lay one window out and waits for its answer;
// an older extension is said to be too old, and the extension's page-host
// news reaches PageHost.
func TestReflowAsksTheExtensionAndWaits(t *testing.T) {
	b, dir := startBridge(t)
	if _, err := b.Reflow("c1-1", WindowBounds{0, 0, 1082, 1995}, 2.625); !errors.Is(err, ErrNoExtensionHost) {
		t.Fatalf("with no extension: %v", err)
	}
	hosts := make(chan [2]string, 1)
	b.PageHost = func(id, host string) { hosts <- [2]string{id, host} }
	for _, version := range []string{"0.3.2", reflowVersion} {
		conn, err := DialBridgeHost(dir, false)
		if err != nil {
			t.Fatal(err)
		}
		enc := json.NewEncoder(conn)
		_ = enc.Encode(hostMessage{Type: "hello", Version: version})
		go func() {
			dec := json.NewDecoder(conn)
			for {
				var m hostMessage
				if dec.Decode(&m) != nil {
					return
				}
				if m.Type != "reflow" {
					continue
				}
				switch {
				case m.ID == "gone":
					_ = enc.Encode(hostMessage{Type: "reflowed", ID: m.ID, Req: m.Req, Message: "the agent window is gone"})
				case m.Zoom == 0 && m.Bounds == nil:
					_ = enc.Encode(hostMessage{Type: "reflowed", ID: m.ID, Req: m.Req})
				case m.Bounds != nil && m.Zoom == 2.625:
					c := *m.Bounds
					c.Top += 80
					_ = enc.Encode(hostMessage{Type: "page", ID: m.ID, Host: "github.com"})
					_ = enc.Encode(hostMessage{Type: "reflowed", ID: m.ID, Req: m.Req, Bounds: &c, Host: "example.com"})
				}
			}
		}()
		for i := 0; i < 200 && b.HostVersion() != version; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		got, err := b.Reflow("c1-1", WindowBounds{0, 0, 1082, 1995}, 2.625)
		if version != reflowVersion {
			if !errors.Is(err, ErrNoReflow) {
				t.Fatalf("extension %s: %v", version, err)
			}
			conn.Close()
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got.Host != "example.com" || got.Content != (WindowBounds{0, 80, 1082, 1995}) {
			t.Fatalf("reflowed %+v", got)
		}
		select {
		case h := <-hosts:
			if h != [2]string{"c1-1", "github.com"} {
				t.Fatalf("page host %v", h)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no page host")
		}
		if _, err := b.Reflow("c1-1", WindowBounds{}, 0); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if _, err := b.Reflow("gone", WindowBounds{0, 0, 1082, 1995}, 2.625); err == nil || err.Error() != "the agent window is gone" {
			t.Fatalf("a window that is gone: %v", err)
		}
		conn.Close()
	}
}
