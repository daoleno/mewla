package agentuse

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func startBridge(t *testing.T) (*Bridge, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "bridge")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := NewBridge(dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go b.Serve(ctx)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(bridgeSocket(dir)); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return b, dir
}

// fakeExtension joins as the host and answers connect messages like the
// extension: it records them and says connected, or an error.
func fakeExtension(t *testing.T, dir string, answer func(m hostMessage) hostMessage) <-chan hostMessage {
	t.Helper()
	conn, err := DialBridgeHost(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	got := make(chan hostMessage, 8)
	enc := json.NewEncoder(conn)
	_ = enc.Encode(hostMessage{Type: "hello", Version: "9.9"})
	go func() {
		dec := json.NewDecoder(conn)
		for {
			var m hostMessage
			if dec.Decode(&m) != nil {
				return
			}
			got <- m
			if m.Type == "connect" {
				reply := answer(m)
				reply.ID = m.ID
				_ = enc.Encode(reply)
			}
		}
	}()
	return got
}

const goodRelay = "ws://127.0.0.1:41234/extension/abc-123"

func waitHost(t *testing.T, b *Bridge) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b.HasHost() && b.HostVersion() == "9.9" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the host never joined")
}

func TestConnectAsksTheExtensionToOpenAnAgentWindow(t *testing.T) {
	b, dir := startBridge(t)
	b.Bounds = func() (WindowBounds, bool) { return WindowBounds{1512, 0, 1600, 1000}, true }
	got := fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "connected"} })
	waitHost(t, b)
	if err := ConnectViaBridge(dir, goodRelay, "claude"); err != nil {
		t.Fatal(err)
	}
	m := <-got
	if m.Type != "connect" || m.RelayURL != goodRelay || m.Client != "claude" {
		t.Fatalf("the extension was asked %+v", m)
	}
	if m.Bounds == nil || *m.Bounds != (WindowBounds{1512, 0, 1600, 1000}) {
		t.Fatalf("the window goes %+v, want the isolated display", m.Bounds)
	}
}

func TestConnectWithoutTheExtensionSaysSo(t *testing.T) {
	_, dir := startBridge(t)
	err := ConnectViaBridge(dir, goodRelay, "claude")
	if err == nil || !strings.Contains(err.Error(), "isn't connected") {
		t.Fatalf("got %v", err)
	}
}

func TestConnectReportsWhyTheExtensionFailed(t *testing.T) {
	b, dir := startBridge(t)
	fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "error", Message: "no window"} })
	waitHost(t, b)
	err := ConnectViaBridge(dir, goodRelay, "claude")
	if err == nil || !strings.Contains(err.Error(), "no window") {
		t.Fatalf("got %v", err)
	}
}

func TestOnlyTheBrowserToolsOwnRelayIsAccepted(t *testing.T) {
	b, dir := startBridge(t)
	got := fakeExtension(t, dir, func(hostMessage) hostMessage { return hostMessage{Type: "connected"} })
	waitHost(t, b)
	for _, bad := range []string{
		"wss://127.0.0.1:41234/extension/x", "ws://evil.example:41234/extension/x",
		"ws://127.0.0.1/extension/x", "ws://127.0.0.1:41234/cdp/x", "http://127.0.0.1:41234/extension/x", "",
	} {
		if err := ConnectViaBridge(dir, bad, "c"); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	select {
	case m := <-got:
		t.Fatalf("the extension was asked %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAConnectIsFailedWhenChromeEndsTheHost(t *testing.T) {
	b, dir := startBridge(t)
	conn, err := DialBridgeHost(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewEncoder(conn).Encode(hostMessage{Type: "hello", Version: "9.9"})
	waitHost(t, b)
	done := make(chan error, 1)
	go func() { done <- ConnectViaBridge(dir, goodRelay, "c") }()
	time.Sleep(100 * time.Millisecond)
	conn.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a connect to a gone extension succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the connect hung after Chrome ended the host")
	}
}

func TestNativeFraming(t *testing.T) {
	var buf bytes.Buffer
	for _, s := range []string{`{"type":"a"}`, `{"type":"b","x":[1,2]}`} {
		if err := writeNative(&buf, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if buf.Bytes()[0] != 12 || buf.Bytes()[1] != 0 {
		t.Fatalf("length bytes % x", buf.Bytes()[:4])
	}
	for _, want := range []string{`{"type":"a"}`, `{"type":"b","x":[1,2]}`} {
		raw, err := readNative(&buf)
		if err != nil || string(raw) != want {
			t.Fatalf("%q %v", raw, err)
		}
	}
	if _, err := readNative(&buf); err != io.EOF {
		t.Fatalf("at the end: %v", err)
	}
	if err := writeNative(io.Discard, make([]byte, maxNativeMessage+1)); err == nil {
		t.Fatal("an oversized message was written")
	}
}

// The native host joins Chrome's framed stdio to the bridge, both ways.
func TestNativeHostJoinsChromeToTheBridge(t *testing.T) {
	b, dir := startBridge(t)
	toHostR, toHostW := io.Pipe()
	var fromHost bytes.Buffer
	out := &lockedWriter{w: &fromHost, mu: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() {
		conn, err := DialBridgeHost(dir)
		if err != nil {
			done <- err
			return
		}
		done <- joinNative(conn, toHostR, out)
	}()
	// The extension says hello through Chrome's stdio.
	if err := writeNative(toHostW, []byte(`{"type":"hello","version":"9.9"}`)); err != nil {
		t.Fatal(err)
	}
	waitHost(t, b)
	// A connect request travels to Chrome, and the extension's answer back.
	answered := make(chan error, 1)
	go func() { answered <- ConnectViaBridge(dir, goodRelay, "claude") }()
	var request hostMessage
	for i := 0; i < 200; i++ {
		raw := out.bytes()
		if len(raw) > 4 {
			if err := json.Unmarshal(raw[4:], &request); err == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request.Type != "connect" || request.RelayURL != goodRelay {
		t.Fatalf("Chrome was sent %+v", request)
	}
	reply, _ := json.Marshal(hostMessage{Type: "connected", ID: request.ID})
	if err := writeNative(toHostW, reply); err != nil {
		t.Fatal(err)
	}
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	toHostW.Close() // Chrome closes the port
	if err := <-done; err != nil {
		t.Fatalf("host ended with %v", err)
	}
	waitNoHost(t, b)
}

func waitNoHost(t *testing.T, b *Bridge) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if !b.HasHost() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the bridge still has a host")
}

type lockedWriter struct {
	mu chan struct{}
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu <- struct{}{}
	defer func() { <-l.mu }()
	return l.w.Write(p)
}

func (l *lockedWriter) bytes() []byte {
	l.mu <- struct{}{}
	defer func() { <-l.mu }()
	return append([]byte(nil), l.w.Bytes()...)
}
