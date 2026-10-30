package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/phone"
	"github.com/gorilla/websocket"
)

// phoneFixture is a daemon with Phone use wired the way main wires it.
type phoneFixture struct {
	t       *testing.T
	s       *Server
	srv     *httptest.Server
	manager *phone.Manager
	agent   *phone.Agent
	use     *agentuse.Service
	sign    func(purpose string) string
	state   string
}

func newPhoneFixture(t *testing.T) *phoneFixture {
	t.Helper()
	a, priv, device := sessionFileAuthFixture(t)
	f := &phoneFixture{t: t, state: t.TempDir()}
	var err error
	if f.use, err = agentuse.New(f.state, false); err != nil {
		t.Fatal(err)
	}
	f.manager = phone.NewManager(phone.Options{})
	f.use.SetPhone(f.manager.Status)
	f.agent = phone.NewAgent(f.manager, phone.NewAudit(f.state))
	f.s = New(a, nil, nil, nil, nil, nil, nil)
	f.s.SetAgentUse(f.use)
	f.s.SetPhone(f.manager)
	f.srv = httptest.NewServer(f.s.Handler())
	t.Cleanup(f.srv.Close)
	f.sign = func(purpose string) string { return calendarAuthHeader(priv, a.DaemonID(), device, purpose) }
	return f
}

func (f *phoneFixture) post(body string) (int, agentuse.PhoneStatus) {
	f.t.Helper()
	sum := sha256.Sum256([]byte(body))
	return f.request(http.MethodPost, "mewla-phone:POST:/phone:sha256="+hex.EncodeToString(sum[:]), body)
}

func (f *phoneFixture) status() agentuse.PhoneStatus {
	f.t.Helper()
	code, status := f.request(http.MethodGet, "mewla-phone:GET:/phone", "")
	if code != http.StatusOK {
		f.t.Fatalf("GET /phone: %d", code)
	}
	return status
}

func (f *phoneFixture) request(method, purpose, body string) (int, agentuse.PhoneStatus) {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.srv.URL+"/phone", strings.NewReader(body))
	req.Header.Set("Authorization", f.sign(purpose))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var status agentuse.PhoneStatus
	_ = json.NewDecoder(resp.Body).Decode(&status)
	return resp.StatusCode, status
}

func (f *phoneFixture) dial(purpose string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	if purpose != "" {
		header.Set("Authorization", f.sign(purpose))
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.srv.URL, "http")+"/phone/device", header)
}

// device is a phone that speaks the protocol over a real WebSocket.
type device struct {
	t        *testing.T
	conn     *websocket.Conn
	mu       sync.Mutex
	requests chan phone.Request
	closed   chan int
	handle   func(phone.Request) *phone.Result
	pings    chan struct{}
}

func (f *phoneFixture) connect(state string, handle func(phone.Request) *phone.Result) *device {
	f.t.Helper()
	conn, _, err := f.dial("mewla-phone-device")
	if err != nil {
		f.t.Fatal(err)
	}
	d := &device{t: f.t, conn: conn, requests: make(chan phone.Request, 32), closed: make(chan int, 1), handle: handle, pings: make(chan struct{}, 32)}
	conn.SetPingHandler(func(string) error {
		d.pings <- struct{}{}
		return conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
	})
	d.send(phone.Hello{Type: phone.TypeHello, Version: phone.Version, Model: "Pixel 8", SDK: 35, AppVersion: "1.2", State: state})
	go d.run()
	f.t.Cleanup(func() { conn.Close() })
	return d
}

func (d *device) send(v any) {
	d.t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.conn.WriteJSON(v); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) run() {
	for {
		kind, data, err := d.conn.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				d.closed <- closeErr.Code
			}
			return
		}
		var req phone.Request
		if kind != websocket.TextMessage || json.Unmarshal(data, &req) != nil || req.Type != phone.TypeRequest {
			continue
		}
		d.requests <- req
		if d.handle == nil {
			continue
		}
		if res := d.handle(req); res != nil {
			res.Type, res.ID = phone.TypeResult, req.ID
			d.send(res)
		}
	}
}

func (d *device) next() phone.Request {
	d.t.Helper()
	select {
	case r := <-d.requests:
		return r
	case <-time.After(3 * time.Second):
		d.t.Fatal("the phone got no request")
		return phone.Request{}
	}
}

func screenWith(text string, x1, y1, x2, y2 int) *phone.Screen {
	return &phone.Screen{W: 1080, H: 2400, Windows: []phone.Window{{Type: 1, Active: true, Pkg: "com.example", Title: "Example", Nodes: []phone.Node{
		{C: "Button", T: text, B: [4]int{x1, y1, x2, y2}, F: phone.FlagClickable | phone.FlagEnabled},
	}}}}
}

func phoneWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPhoneSwitchIsSignedOverItsBody(t *testing.T) {
	f := newPhoneFixture(t)
	if st := f.status(); st.Enabled || st.Connected || st.State != phone.StateOffline {
		t.Fatalf("Phone use is off until turned on, and nothing is connected: %+v", st)
	}
	// Another purpose, or no signature, reads nothing.
	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/phone", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned: %d", resp.StatusCode)
	}
	if code, _ := f.request(http.MethodGet, "mewla-agent-use:GET:/agent-use", ""); code != http.StatusUnauthorized {
		t.Fatalf("another purpose read the phone: %d", code)
	}
	on := `{"enabled":true}`
	// A signature over one body cannot carry another.
	sum := sha256.Sum256([]byte(`{"enabled":false}`))
	if code, _ := f.request(http.MethodPost, "mewla-phone:POST:/phone:sha256="+hex.EncodeToString(sum[:]), on); code != http.StatusUnauthorized {
		t.Fatalf("body swapped under a signature: %d", code)
	}
	if code, st := f.post(on); code != http.StatusOK || !st.Enabled || !f.use.PhoneReady() {
		t.Fatalf("turn on: %d %+v", code, st)
	}
	if !f.status().Enabled {
		t.Fatal("the switch did not stay on")
	}
	// It is also in the agent-use status the app already reads.
	if row := f.use.Status().Phone; row == nil || !row.Enabled {
		t.Fatalf("agent-use status: %+v", row)
	}
	for _, bad := range []string{`{}`, `{"enabled":true,"stop_agent":true}`, `{"enabled":"yes"}`, `{"other":1}`, `nope`} {
		if code, _ := f.post(bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", bad, code)
		}
	}
	if code, st := f.post(`{"enabled":false}`); code != http.StatusOK || st.Enabled || f.use.PhoneReady() {
		t.Fatalf("turn off: %d %+v", code, st)
	}
	req, _ = http.NewRequest(http.MethodDelete, f.srv.URL+"/phone", nil)
	req.Header.Set("Authorization", f.sign("mewla-phone:GET:/phone"))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", resp.StatusCode)
	}
}

func TestAnAgentUsesThePhoneThroughTheDaemonOverARealWebSocket(t *testing.T) {
	f := newPhoneFixture(t)
	if _, err := f.use.SetPhoneEnabled(true); err != nil {
		t.Fatal(err)
	}

	// Only a paired device, with the right purpose, may connect.
	if _, resp, err := f.dial(""); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned connection: %v", err)
	}
	if _, resp, err := f.dial("mewla-screen-view"); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("another purpose connected: %v", err)
	}

	home := screenWith("Compose", 100, 200, 300, 400)
	next := screenWith("Sent", 0, 0, 100, 100)
	var mu sync.Mutex
	current := home
	d := f.connect(phone.StateReady, func(r phone.Request) *phone.Result {
		mu.Lock()
		defer mu.Unlock()
		res := &phone.Result{OK: true}
		switch r.Op {
		case phone.OpScreen:
			res.Screen = current
		case phone.OpTap:
			current = next
			res.Screen = current
		case phone.OpRelease:
		default:
			res.Screen = current
		}
		return res
	})
	phoneWaitFor(t, "the phone to connect", func() bool { return f.status().Connected })
	st := f.status()
	if st.State != phone.StateReady || st.Model != "Pixel 8" || st.SDK != 35 || st.AppVersion != "1.2" {
		t.Fatalf("status %+v", st)
	}

	ctx := context.Background()
	res, err := f.agent.Act(ctx, "%7", "Writer", phone.AgentAction{Action: phone.ActScreen})
	if err != nil || !strings.Contains(res.Text, `[1] button "Compose" (200,300) tap`) {
		t.Fatalf("screen: %q %v", res.Text, err)
	}
	if req := d.next(); req.Op != phone.OpScreen || req.Worker != "%7" || req.WorkerName != "Writer" {
		t.Fatalf("request %+v", req)
	}
	st = f.status()
	if st.Active == nil || st.Active.Worker != "%7" || st.Active.Name != "Writer" {
		t.Fatalf("the app does not see the agent: %+v", st.Active)
	}
	if st.W != 1080 || st.H != 2400 {
		t.Fatalf("display size %dx%d", st.W, st.H)
	}

	res, err = f.agent.Act(ctx, "%7", "Writer", phone.AgentAction{Action: phone.ActTap, Element: 1})
	if err != nil || !strings.Contains(res.Text, `button "Sent"`) {
		t.Fatalf("tap: %q %v", res.Text, err)
	}
	req := d.next()
	var tapArgs phone.TapArgs
	_ = json.Unmarshal(req.Args, &tapArgs)
	if req.Op != phone.OpTap || tapArgs.X != 200 || tapArgs.Y != 300 {
		t.Fatalf("tap request %+v %+v", req, tapArgs)
	}

	// The person's Stop from the app: the agent is refused, and the phone is told to let go.
	if code, st := f.post(`{"stop_agent":true}`); code != http.StatusOK || st.State != phone.StateStopped || st.Active != nil {
		t.Fatalf("stop: %d %+v", code, st)
	}
	if req := d.next(); req.Op != phone.OpRelease {
		t.Fatalf("after Stop the phone got %+v", req)
	}
	if _, err := f.agent.Act(ctx, "%7", "Writer", phone.AgentAction{Action: phone.ActScreen}); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("after Stop: %v", err)
	}
	d.send(phone.Event{Type: phone.TypeEvent, Kind: phone.EventGiveBack})
	phoneWaitFor(t, "give_back", func() bool { return f.status().State == phone.StateReady })
	if _, err := f.agent.Act(ctx, "%7", "Writer", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatalf("after give_back: %v", err)
	}
	d.next()

	// The person takes over from the phone.
	d.send(phone.State{Type: phone.TypeState, State: phone.StatePersonInCharge})
	phoneWaitFor(t, "take over", func() bool { return f.status().State == phone.StatePersonInCharge })
	if _, err := f.agent.Act(ctx, "%7", "Writer", phone.AgentAction{Action: phone.ActScreen}); err == nil || !strings.Contains(err.Error(), "do not retry") {
		t.Fatalf("person in control: %v", err)
	}
	d.send(phone.State{Type: phone.TypeState, State: phone.StateReady})
	phoneWaitFor(t, "ready", func() bool { return f.status().State == phone.StateReady })

	// Turning Phone use off lets go of the phone.
	if code, st := f.post(`{"enabled":false}`); code != http.StatusOK || st.Enabled || st.Active != nil {
		t.Fatalf("off: %d %+v", code, st)
	}
	phoneWaitFor(t, "release after off", func() bool {
		select {
		case r := <-d.requests:
			return r.Op == phone.OpRelease
		default:
			return false
		}
	})
}

func TestANewerPhoneConnectionReplacesTheOlderAndSaysWhy(t *testing.T) {
	f := newPhoneFixture(t)
	first := f.connect(phone.StateReady, nil)
	phoneWaitFor(t, "first phone", func() bool { return f.status().Connected })
	second := f.connect(phone.StateReady, okScreen())
	phoneWaitFor(t, "replacement", func() bool { return f.manager.Generation() == 2 })
	select {
	case code := <-first.closed:
		if code != phone.CloseReplaced {
			t.Fatalf("the old connection closed with %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the old connection was not closed")
	}
	if _, err := f.agent.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatal(err)
	}
	if req := second.next(); req.Op != phone.OpScreen {
		t.Fatalf("%+v", req)
	}
	// The old connection ending does not take the new one down.
	if st := f.status(); !st.Connected {
		t.Fatal("the new connection was lost with the old")
	}
}

func okScreen() func(phone.Request) *phone.Result {
	return func(phone.Request) *phone.Result {
		return &phone.Result{OK: true, Screen: screenWith("x", 0, 0, 10, 10)}
	}
}

func TestAPhoneThatSpeaksOtherwiseIsTurnedAway(t *testing.T) {
	f := newPhoneFixture(t)
	good := f.connect(phone.StateReady, nil)
	phoneWaitFor(t, "phone", func() bool { return f.status().Connected })

	closeCode := func(conn *websocket.Conn) int {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				var closeErr *websocket.CloseError
				if errors.As(err, &closeErr) {
					return closeErr.Code
				}
				t.Fatalf("read: %v", err)
			}
		}
	}
	conn, _, err := f.dial("mewla-phone-device")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(phone.Hello{Type: phone.TypeHello, Version: phone.Version + 1, State: phone.StateReady})
	if code := closeCode(conn); code != phone.CloseUnsupported {
		t.Fatalf("a newer protocol: %d", code)
	}
	conn, _, _ = f.dial("mewla-phone-device")
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"state","state":"ready"}`))
	if code := closeCode(conn); code != phone.CloseProtocol {
		t.Fatalf("no hello: %d", code)
	}
	conn, _, _ = f.dial("mewla-phone-device")
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3})
	if code := closeCode(conn); code != phone.CloseProtocol {
		t.Fatalf("binary hello: %d", code)
	}
	// Garbage after the hello ends that connection only.
	bad := f.connect(phone.StateReady, nil)
	phoneWaitFor(t, "second phone", func() bool { return f.manager.Generation() >= 2 })
	bad.mu.Lock()
	_ = bad.conn.WriteMessage(websocket.TextMessage, []byte("not json"))
	bad.mu.Unlock()
	select {
	case code := <-bad.closed:
		if code != phone.CloseProtocol {
			t.Fatalf("garbage: %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("garbage was tolerated")
	}
	select {
	case code := <-good.closed:
		if code != phone.CloseReplaced {
			t.Fatalf("%d", code)
		}
	case <-time.After(3 * time.Second):
	}
}

func TestThePhoneIsPingedAndAJpegResultFits(t *testing.T) {
	f := newPhoneFixture(t)
	f.s.phonePing = 30 * time.Millisecond
	image := bytes.Repeat([]byte{0xff, 0xd8, 0x00}, 1<<20) // a few megabytes
	d := f.connect(phone.StateReady, func(r phone.Request) *phone.Result {
		return &phone.Result{OK: true, Screen: &phone.Screen{W: 100, H: 100}, Image: base64Of(image)}
	})
	select {
	case <-d.pings:
	case <-time.After(3 * time.Second):
		t.Fatal("the daemon never pinged the phone")
	}
	phoneWaitFor(t, "phone", func() bool { return f.status().Connected })
	res, err := f.agent.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen, Image: true})
	if err != nil || !bytes.Equal(res.Image, image) {
		t.Fatalf("a %d byte picture came back as %d bytes: %v", len(image), len(res.Image), err)
	}
}

func base64Of(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
