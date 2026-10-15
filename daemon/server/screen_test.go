package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/gorilla/websocket"
)

type screenViewerClient struct {
	t    *testing.T
	conn *websocket.Conn
	buf  []byte
}

// read returns the next n RFB bytes, skipping JSON control messages.
func (c *screenViewerClient) read(n int) []byte {
	c.t.Helper()
	for len(c.buf) < n {
		_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		kind, data, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("read RFB: %v", err)
		}
		if kind == websocket.BinaryMessage {
			c.buf = append(c.buf, data...)
		}
	}
	out := c.buf[:n]
	c.buf = c.buf[n:]
	return out
}

func (c *screenViewerClient) write(data []byte) {
	c.t.Helper()
	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		c.t.Fatal(err)
	}
}

func TestScreenNeedsApprovalThenBridgesTheBackend(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(stateDir, fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	wsURL := "ws" + strings.TrimPrefix(h.URL, "http") + "/screen/viewer"
	dial := func(purpose string) (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		if purpose != "" {
			header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		}
		return websocket.DefaultDialer.Dial(wsURL, header)
	}
	call := func(method, body string) (screen.Status, int, string) {
		req, _ := http.NewRequest(method, h.URL+"/screen", strings.NewReader(body))
		req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var status screen.Status
		_ = json.Unmarshal(raw, &status)
		return status, resp.StatusCode, string(raw)
	}

	for _, purpose := range []string{"", "mewla-connect", "mewla-screen"} {
		if c, resp, err := dial(purpose); err == nil {
			c.Close()
			t.Fatalf("purpose %q opened the screen", purpose)
		} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("purpose %q: %v", purpose, err)
		}
	}
	unsigned, _ := http.NewRequest(http.MethodGet, h.URL+"/screen", nil)
	unsigned.Header.Set("Accept", "application/json")
	if resp, err := http.DefaultClient.Do(unsigned); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned status: %v %v", resp, err)
	}

	status, code, _ := call(http.MethodGet, "")
	if code != 200 || !status.Supported || status.Approved || status.Ready {
		t.Fatalf("status before approval: %d %+v", code, status)
	}
	// A device that was never allowed is told so and closed.
	c, _, err := dial("mewla-screen-view")
	if err != nil {
		t.Fatal(err)
	}
	var msg struct{ Type, Error string }
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.ReadJSON(&msg); err != nil || msg.Type != "error" || !strings.Contains(msg.Error, "isn't allowed") {
		t.Fatalf("unapproved viewer: %+v %v", msg, err)
	}
	c.Close()
	if _, code, body := call(http.MethodPost, `{"keep_awake":false}`); code != http.StatusConflict {
		t.Fatalf("unapproved keep-awake: %d %s", code, body)
	}
	if _, code, _ := call(http.MethodPost, `{"action":"remote_enable_everything"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown action: %d", code)
	}

	// Enabling asks the computer once, then the device is ready.
	status, code, body := call(http.MethodPost, `{"action":"enable"}`)
	if code != 200 || !status.Approved || !status.Ready || len(fake.Confirmed) != 1 {
		t.Fatalf("enable: %d %s %v", code, body, fake.Confirmed)
	}
	if _, code, _ := call(http.MethodPost, `{"action":"open_settings","permission":"accessibility"}`); code != 200 || fake.Opened[0] != "accessibility" {
		t.Fatalf("open settings: %d %v", code, fake.Opened)
	}

	open := func() *screenViewerClient {
		conn, _, err := dial("mewla-screen-view")
		if err != nil {
			t.Fatal(err)
		}
		v := &screenViewerClient{t: t, conn: conn}
		if got := string(v.read(12)); got != "RFB 003.008\n" {
			t.Fatalf("version %q", got)
		}
		v.write([]byte("RFB 003.008\n"))
		if got := v.read(2); got[0] != 1 || got[1] != 1 {
			t.Fatalf("viewer was offered %v, want only None", got)
		}
		v.write([]byte{1})
		if got := v.read(4); binary.BigEndian.Uint32(got) != 0 {
			t.Fatalf("security result %v", got)
		}
		v.write([]byte{1}) // ClientInit
		init := v.read(24)
		if binary.BigEndian.Uint16(init[0:]) != 320 || binary.BigEndian.Uint16(init[2:]) != 200 {
			t.Fatalf("ServerInit %v", init)
		}
		v.read(int(binary.BigEndian.Uint32(init[20:])))
		return v
	}
	first := open()
	first.write([]byte{5, 1, 0, 10, 0, 20}) // pointer press at (10, 20)
	if err := first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","t":42}`)); err != nil {
		t.Fatal(err)
	}
	var pong struct {
		Type string
		T    int
	}
	for pong.Type != "pong" {
		_ = first.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := first.conn.ReadJSON(&pong); err != nil {
			t.Fatal(err)
		}
	}
	if pong.T != 42 {
		t.Fatalf("pong %+v", pong)
	}
	if pointers, _ := fake.Snapshot(); len(pointers) != 1 || pointers[0] != (screen.FakePointer{X: 10, Y: 20, Buttons: 1}) {
		t.Fatalf("pointer did not reach the backend: %+v", pointers)
	}

	// A second viewer takes over; the first is closed with the reason.
	second := open()
	defer second.conn.Close()
	_ = first.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := first.conn.ReadMessage()
		if err == nil {
			continue
		}
		if !websocket.IsCloseError(err, websocket.CloseNormalClosure) || !strings.Contains(err.Error(), "took over") {
			t.Fatalf("first viewer close: %v", err)
		}
		break
	}

	// The local kill switch ends the open session.
	if err := service.Disable(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	second.write([]byte{5, 0, 0, 1, 0, 1})
	_ = second.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		kind, data, err := second.conn.ReadMessage()
		if err != nil {
			t.Fatal("closed without saying why")
		}
		if kind == websocket.TextMessage && strings.Contains(string(data), "isn't allowed") {
			break
		}
	}
}

func TestRevokingADeviceForgetsItsScreenApproval(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	service := screen.NewServiceWith(stateDir, screen.NewFakeBackend(320, 200))
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	req, _ := http.NewRequest(http.MethodPost, h.URL+"/screen", strings.NewReader(`{"action":"enable"}`))
	req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: %v %v", resp, err)
	}
	resp.Body.Close()
	if cfg, _ := screen.LoadConfig(stateDir); !cfg.Approved(device) || !cfg.KeepAwake() {
		t.Fatalf("config after enable = %+v", cfg)
	}

	if _, err := a.RevokeDevice(device); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := screen.LoadConfig(stateDir); cfg.Approved(device) || cfg.KeepAwake() {
		t.Fatalf("revoked device still approved: %+v", cfg)
	}
}

func TestStaleScreenApprovalIsForgottenAtStartup(t *testing.T) {
	a, _, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	if err := screen.SaveConfig(stateDir, screen.Config{Devices: map[string]screen.Approval{
		device:    {Name: "Pixel"},
		"revoked": {Name: "Old phone"},
	}}); err != nil {
		t.Fatal(err)
	}
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(screen.NewServiceWith(stateDir, screen.NewFakeBackend(32, 32)))
	cfg, _ := screen.LoadConfig(stateDir)
	if !cfg.Approved(device) || cfg.Approved("revoked") {
		t.Fatalf("config after startup = %+v", cfg)
	}
}

// Watching an agent shows its screen without taking over: the agent keeps
// control, the viewer's input goes nowhere, and the viewer hears when the
// agent is done.
func TestWatchingAnAgentLeavesItInControl(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	defer service.StopAgent()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	req, _ := http.NewRequest(http.MethodPost, h.URL+"/screen", strings.NewReader(`{"action":"enable"}`))
	req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("enable: %v %v", resp, err)
	}
	dial := func() *screenViewerClient {
		header := http.Header{}
		header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer?watch=1", header)
		if err != nil {
			t.Fatal(err)
		}
		return &screenViewerClient{t: t, conn: conn}
	}
	readError := func(v *screenViewerClient) string {
		t.Helper()
		for {
			_ = v.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			kind, data, err := v.conn.ReadMessage()
			if err != nil {
				t.Fatalf("closed without saying why: %v", err)
			}
			var msg struct{ Type, Error string }
			if kind == websocket.TextMessage && json.Unmarshal(data, &msg) == nil && msg.Type == "error" {
				return msg.Error
			}
		}
	}

	nobody := dial()
	if got := readError(nobody); !strings.Contains(got, "No agent") {
		t.Fatalf("watching nobody: %q", got)
	}
	nobody.conn.Close()

	if _, err := service.Act(context.Background(), "%7", screen.AgentAction{Action: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	v := dial()
	defer v.conn.Close()
	v.read(12)
	v.write([]byte("RFB 003.008\n"))
	v.read(2)
	v.write([]byte{1})
	v.read(4)
	v.write([]byte{1}) // ClientInit
	init := v.read(24)
	if binary.BigEndian.Uint16(init[0:]) != 320 || binary.BigEndian.Uint16(init[2:]) != 200 {
		t.Fatalf("ServerInit %v", init)
	}
	v.read(int(binary.BigEndian.Uint32(init[20:])))
	v.write([]byte{3, 0, 0, 0, 0, 0, 1, 64, 0, 200}) // FramebufferUpdateRequest
	v.read(4)                                        // the update arrives
	v.write([]byte{5, 1, 0, 10, 0, 20})              // a tap
	v.write([]byte{4, 1, 0, 0, 0, 0, 0, 'q'})        // a key
	time.Sleep(100 * time.Millisecond)
	if pointers, keys := fake.Snapshot(); len(pointers) != 0 || len(keys) != 0 {
		t.Fatalf("a watcher's input reached the screen: %v %v", pointers, keys)
	}
	if state, ok := service.Agent(); !ok || state.Worker != "%7" {
		t.Fatal("watching took the screen from the agent")
	}

	service.StopAgent()
	if got := readError(v); !strings.Contains(got, "agent is done") {
		t.Fatalf("agent ended: %q", got)
	}
}
