package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/gorilla/websocket"
)

func TestScreenNeedsApprovalThenBridgesTheBackend(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(stateDir, fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := serveViewers(t, s.Handler())
	wsURL := "ws" + strings.TrimPrefix(h.URL, "http") + "/screen/viewer?video=webrtc"
	dial := func(purpose string) (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		if purpose != "" {
			header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		}
		return websocket.DefaultDialer.Dial(wsURL, header)
	}
	call := func(method, body string) (screen.Status, int, string) {
		req, _ := http.NewRequest(method, h.URL+"/screen?video=webrtc", strings.NewReader(body))
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

	open := func() *videoViewer {
		header := http.Header{}
		header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
		v := dialVideoViewer(t, wsURL, viewerIdentity(priv, a, device))
		v.next("ready")
		v.picture()
		return v
	}
	// The pong answers the ping in the socket's reader; the press goes on through the input channel to the
	// backend, so the pong is no sign it has arrived. The backend says when it has.
	pointed := make(chan struct{})
	var pointedOnce sync.Once
	fake.AfterPointer = func(int) { pointedOnce.Do(func() { close(pointed) }) }
	first := open()
	first.send([]byte{5, 1, 0, 10, 0, 20}) // pointer press at (10, 20)
	first.sendJSON(map[string]any{"type": "ping", "t": 42})
	var pong struct {
		Type string
		T    int
	}
	_ = json.Unmarshal(first.next("pong"), &pong)
	if pong.T != 42 {
		t.Fatalf("pong %+v", pong)
	}
	select {
	case <-pointed:
	case <-time.After(5 * time.Second):
		t.Fatal("the pointer never reached the backend")
	}
	if pointers, _ := fake.Snapshot(); len(pointers) != 1 || pointers[0] != (screen.FakePointer{X: 10, Y: 20, Buttons: 1}) {
		t.Fatalf("pointer did not reach the backend: %+v", pointers)
	}

	// A second viewer takes over; the first is closed with the reason.
	second := open()
	if err := first.closedWith(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("first viewer close: %v", err)
	}

	// The first viewer's deferred release leaves the second in control: its
	// input lands, and an agent's action is refused.
	time.Sleep(300 * time.Millisecond)
	second.send([]byte{5, 0, 0, 30, 0, 40}) // a move to (30, 40)
	deadline := time.Now().Add(5 * time.Second)
	for {
		pointers, _ := fake.Snapshot()
		if len(pointers) > 0 && pointers[len(pointers)-1] == (screen.FakePointer{X: 30, Y: 40}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the second viewer's input did not land: %+v", pointers)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := service.Act(context.Background(), "w9", screen.AgentAction{Action: "left_click", Coordinate: []int{1, 1}}); !errors.Is(err, screen.ErrPersonControlling) {
		t.Fatalf("an agent's action while the second viewer held the screen: %v", err)
	}

	// The local kill switch ends the open session.
	if err := service.Disable(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	second.sendJSON(map[string]any{"type": "ping", "t": 1})
	if got := second.errorText(); !strings.Contains(got, "isn't allowed") {
		t.Fatalf("closed saying %q", got)
	}
}

func TestRevokingADeviceForgetsItsScreenApproval(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	service := screen.NewServiceWith(stateDir, screen.NewFakeBackend(320, 200))
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := serveViewers(t, s.Handler())
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
	s.SetScreenLoopbackOnly(true)
	h := serveViewers(t, s.Handler())
	req, _ := http.NewRequest(http.MethodPost, h.URL+"/screen", strings.NewReader(`{"action":"enable"}`))
	req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("enable: %v %v", resp, err)
	}
	dial := func() *videoViewer {
		header := http.Header{}
		header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
		return dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer?watch=1", viewerIdentity(priv, a, device))
	}

	nobody := dial()
	if got := nobody.errorText(); !strings.Contains(got, "No agent") {
		t.Fatalf("watching nobody: %q", got)
	}
	nobody.close()

	if _, err := service.Act(context.Background(), "%7", screen.AgentAction{Action: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	v := dial()
	v.next("ready")
	v.picture()
	v.send([]byte{5, 1, 0, 10, 0, 20})       // a tap
	v.send([]byte{4, 1, 0, 0, 0, 0, 0, 'q'}) // a key
	// Watching accepts input: it reaches the screen, takes nothing from the
	// agent, and the agent hears about it with its next answer.
	deadline := time.Now().Add(3 * time.Second)
	for {
		pointers, keys := fake.Snapshot()
		if len(pointers) == 1 && len(keys) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a watcher's input did not reach the screen: %v %v", pointers, keys)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state, ok := service.Agent(); !ok || state.Worker != "%7" {
		t.Fatal("watching took the screen from the agent")
	}
	told, err := service.Act(context.Background(), "%7", screen.AgentAction{Action: "cursor_position"})
	if err != nil || !strings.Contains(told.Text, "person used") || !strings.Contains(told.Text, "click") {
		t.Fatalf("the agent was not told what the watcher did: %v %q", err, told.Text)
	}

	service.StopAgent()
	if got := v.errorText(); !strings.Contains(got, "agent is done") {
		t.Fatalf("agent ended: %q", got)
	}
}

// The viewer is told when the computer is locked, and how its stream does:
// the path it arrived by and what each stage of an update cost.
func TestScreenViewerHearsLockStateAndStreamStats(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := serveViewers(t, s.Handler())
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	fake.SetLocked(true)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
	v := dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer", viewerIdentity(priv, a, device))
	var lockState struct {
		Locked bool `json:"locked"`
	}
	_ = json.Unmarshal(v.next("state"), &lockState)
	if !lockState.Locked {
		t.Fatal("the viewer was not told the computer is locked")
	}
	fake.SetLocked(false)
	_ = json.Unmarshal(v.next("state"), &lockState)
	if lockState.Locked {
		t.Fatal("the viewer was not told the computer is unlocked")
	}
	v.picture()
	var stats struct {
		Path    string  `json:"path"`
		Encoder string  `json:"encoder"`
		Updates int     `json:"updates"`
		Bytes   int     `json:"bytes"`
		Encode  float64 `json:"encode_ms"`
		Route   string  `json:"route"`
	}
	// The screen keeps changing: stats that counted the first picture may have
	// gone by while the lock state was awaited (a slow runner).
	deadline := time.Now().Add(10 * time.Second)
	for stats.Updates == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no stats with updates: %+v", stats)
		}
		fake.ScrollTick()
		_ = json.Unmarshal(v.next("stats"), &stats)
	}
	if stats.Path != "loopback" || stats.Encoder == "" || stats.Bytes == 0 || stats.Encode <= 0 {
		t.Fatalf("stats %+v", stats)
	}
}

// A viewer on the computer itself is told so, and nobody else: a live view of the
// screen from the screen's own computer is a mirror of the viewer.
func TestScreenStatusSaysWhetherTheAskerIsOnThisComputer(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(screen.NewServiceWith(t.TempDir(), screen.NewFakeBackend(320, 200)))
	ask := func(remote, host string, header map[string]string) screen.Status {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/screen", nil)
		req.RemoteAddr, req.Host = remote, host
		req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var status screen.Status
		if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if !ask("127.0.0.1:4000", "127.0.0.1:9876", nil).Local {
		t.Error("the web UI on this computer was not local")
	}
	if ask("100.73.112.9:4000", "100.73.112.8:9877", nil).Local {
		t.Error("a phone on the tailnet was local")
	}
	if ask("127.0.0.1:4000", "127.0.0.1:9876", map[string]string{"X-Forwarded-For": "203.0.113.7"}).Local {
		t.Error("a request relayed by a proxy on this computer was local")
	}
}
