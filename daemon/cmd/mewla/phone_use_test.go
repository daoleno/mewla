package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/phone"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPhoneMCPHelper(t *testing.T) {
	if os.Getenv("MEWLA_PHONE_MCP_FIXTURE") != "1" {
		return
	}
	if err := runAgentUseCommand([]string{"phone-mcp", "--state-dir", os.Getenv("MEWLA_PHONE_FIXTURE_STATE")}, os.Stderr); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

// answeringPhone is a phone connection that answers each request through handle.
type answeringPhone struct {
	mu       sync.Mutex
	link     *phone.Link
	requests []phone.Request
	handle   func(phone.Request) *phone.Result
}

func (p *answeringPhone) Send(frame []byte) error {
	var req phone.Request
	if err := json.Unmarshal(frame, &req); err != nil {
		return err
	}
	p.mu.Lock()
	p.requests = append(p.requests, req)
	handle, link := p.handle, p.link
	p.mu.Unlock()
	if res := handle(req); res != nil {
		res.Type, res.ID = phone.TypeResult, req.ID
		raw, _ := json.Marshal(res)
		return link.Receive(raw)
	}
	return nil
}

func (p *answeringPhone) Close(int, string) {}

func (p *answeringPhone) ops() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ops []string
	for _, r := range p.requests {
		ops = append(ops, r.Op)
	}
	return ops
}

// A real MCP client launches the real `mewla agent-use phone-mcp`, which reaches the daemon's
// handler over the real control socket. Only the phone and Worker liveness are stand-ins.
func TestPhoneUseThroughTheControlSocket(t *testing.T) {
	root := t.TempDir()
	agents, err := agentuse.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	manager := phone.NewManager(phone.Options{})
	agents.SetPhone(manager.Status)
	picture := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff\xe0jpeg"))
	device := &answeringPhone{}
	device.handle = func(r phone.Request) *phone.Result {
		res := &phone.Result{OK: true, Screen: &phone.Screen{W: 1080, H: 2400, Windows: []phone.Window{{Type: 1, Active: true, Pkg: "com.example", Title: "Example", Nodes: []phone.Node{
			{C: "Button", T: "Compose", B: [4]int{100, 200, 300, 400}, F: phone.FlagClickable | phone.FlagEnabled},
			{C: "EditText", H: "To", B: [4]int{0, 500, 1000, 600}, F: phone.FlagEditable | phone.FlagEnabled},
		}}}}}
		switch r.Op {
		case phone.OpApps:
			res.Apps = []phone.App{{Package: "com.example", Label: "Example"}}
		case phone.OpHandoff:
			res.Note = "The person finished."
		}
		if r.Image {
			res.Image = picture
		}
		return res
	}
	link, err := manager.Attach(phone.Hello{Type: phone.TypeHello, Version: phone.Version, Model: "Pixel 8", SDK: 35, State: phone.StateReady}, device)
	if err != nil {
		t.Fatal(err)
	}
	device.link = link

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := newFakeControlWatcher()
	watcher.workers["%7"] = &classifier.Worker{ID: "%7", Name: "reply-writer", State: classifier.StateRunning}
	app := &controlApp{phone: phone.NewAgent(manager, phone.NewAudit(root)), agentUse: agents, watcher: watcher}
	cs := &control.Server{Path: filepath.Join(root, "run", "control.sock"), Handler: app}
	controlDone := make(chan error, 1)
	go func() { controlDone <- cs.Run(ctx) }()
	defer func() { cancel(); <-controlDone }()
	for i := 0; i < 100; i++ {
		if _, e := os.Stat(cs.Path); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	attach := func(worker string) *mcp.ClientSession {
		t.Helper()
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestPhoneMCPHelper$")
		cmd.Env = append(os.Environ(), "MEWLA_PHONE_MCP_FIXTURE=1", "MEWLA_PHONE_FIXTURE_STATE="+root, "MEWLA_WORKER_ID="+worker)
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-agent", Version: "1"}, nil)
		session, e := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	session := attach("%7")
	defer session.Close()
	call := func(s *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
		t.Helper()
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(r)
		return r, string(raw)
	}
	text := func(r *mcp.CallToolResult) string {
		for _, c := range r.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				return tc.Text
			}
		}
		return ""
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if len(tool.Description) > 900 {
			t.Errorf("%s: a description of %d characters is not terse", tool.Name, len(tool.Description))
		}
	}
	want := "phone_apps,phone_handoff,phone_key,phone_launch,phone_screen,phone_steps,phone_swipe,phone_tap,phone_type,phone_wait"
	if got := strings.Join(sortedStrings(names), ","); got != want {
		t.Fatalf("tools %s", got)
	}

	// Phone use is off until the person turns it on, and the tool says where.
	if r, raw := call(session, "phone_screen", nil); !r.IsError || !strings.Contains(raw, "Phone use is off: turn it on in Settings → Labs") {
		t.Fatalf("off: %s", raw)
	}
	if _, err := agents.SetPhoneEnabled(true); err != nil {
		t.Fatal(err)
	}

	r, raw := call(session, "phone_screen", nil)
	if r.IsError || strings.Contains(raw, "structuredContent") || !strings.Contains(text(r), `[1] button "Compose" (200,300) tap`) {
		t.Fatalf("screen: %s", raw)
	}
	if status := agents.Status(); status.Phone == nil || status.Phone.Active == nil || status.Phone.Active.Worker != "%7" || status.Phone.Active.Name != "reply-writer" {
		t.Fatalf("the app does not see the agent: %+v", status.Phone)
	}

	// A picture comes as image content, like the computer's, and only when asked or needed.
	r, raw = call(session, "phone_screen", map[string]any{"image": true})
	image, ok := r.Content[0].(*mcp.ImageContent)
	if !ok || image.MIMEType != "image/jpeg" || string(image.Data) != "\xff\xd8\xff\xe0jpeg" || strings.Contains(raw, "structuredContent") {
		t.Fatalf("picture: %s", raw)
	}

	if r, raw := call(session, "phone_tap", map[string]any{"element": 1}); r.IsError || !strings.Contains(text(r), "Compose") {
		t.Fatalf("tap: %s", raw)
	}
	const secret = "meet me at the old pier"
	if r, raw := call(session, "phone_type", map[string]any{"element": 2, "text": secret, "submit": true}); r.IsError {
		t.Fatalf("type: %s", raw)
	}
	for _, c := range []struct {
		tool string
		args map[string]any
		op   string
	}{
		{"phone_swipe", map[string]any{"direction": "up"}, phone.OpSwipe},
		{"phone_key", map[string]any{"key": "back"}, phone.OpKey},
		{"phone_wait", map[string]any{"seconds": 1}, phone.OpWait},
		{"phone_apps", nil, phone.OpApps},
		{"phone_launch", map[string]any{"app": "example"}, phone.OpLaunch},
	} {
		if r, raw := call(session, c.tool, c.args); r.IsError {
			t.Fatalf("%s: %s", c.tool, raw)
		}
		device.mu.Lock()
		last := device.requests[len(device.requests)-1].Op
		device.mu.Unlock()
		if last != c.op {
			t.Fatalf("%s sent %s", c.tool, last)
		}
	}
	// A batch is one call that ends on one screen.
	if r, raw := call(session, "phone_steps", map[string]any{"steps": []map[string]any{{"action": "key", "key": "back"}, {"action": "tap", "element": 1}}}); r.IsError || !strings.Contains(text(r), "2. tap: done") {
		t.Fatalf("steps: %s", raw)
	}
	// A handoff waits for the person; here the phone answers at once.
	if r, raw := call(session, "phone_handoff", map[string]any{"reason": "Sign in"}); r.IsError || !strings.Contains(text(r), "Compose") {
		t.Fatalf("handoff: %s", raw)
	}

	// A person comes first, and the agent is told to stop, not to retry.
	sendToPhone := func(v any) {
		raw, _ := json.Marshal(v)
		if err := link.Receive(raw); err != nil {
			t.Fatal(err)
		}
	}
	sendToPhone(phone.Event{Type: phone.TypeEvent, Kind: phone.EventTakeOver})
	if r, raw := call(session, "phone_tap", map[string]any{"x": 5, "y": 5}); !r.IsError || !strings.Contains(raw, "person") || !strings.Contains(raw, "do not retry") {
		t.Fatalf("person in control: %s", raw)
	}
	sendToPhone(phone.Event{Type: phone.TypeEvent, Kind: phone.EventGiveBack})
	manager.Stop()
	if r, raw := call(session, "phone_screen", nil); !r.IsError || !strings.Contains(raw, "stopped") {
		t.Fatalf("stopped: %s", raw)
	}
	sendToPhone(phone.Event{Type: phone.TypeEvent, Kind: phone.EventGiveBack})
	if r, raw := call(session, "phone_screen", nil); r.IsError {
		t.Fatalf("after give_back: %s", raw)
	}

	// Only a running Mewla Worker may act.
	ghost := attach("%99")
	defer ghost.Close()
	if r, raw := call(ghost, "phone_screen", nil); !r.IsError || !strings.Contains(raw, "running Mewla Worker") {
		t.Fatalf("unknown worker: %s", raw)
	}

	// The audit holds what was done, never what was typed.
	audit, err := os.ReadFile(filepath.Join(root, phone.AuditFile))
	if err != nil || strings.Contains(string(audit), "old pier") || !strings.Contains(string(audit), `"worker":"%7"`) || !strings.Contains(string(audit), `"text_length":23`) {
		t.Fatalf("audit %v: %s", err, audit)
	}

	// Turning Phone use off from anywhere ends it for the Worker at once.
	if _, err := agents.SetPhoneEnabled(false); err != nil {
		t.Fatal(err)
	}
	if r, raw := call(session, "phone_tap", map[string]any{"x": 5, "y": 5}); !r.IsError || !strings.Contains(raw, "Phone use is off") {
		t.Fatalf("off again: %s", raw)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Each layer that waits for a handoff waits longer than the one inside it, up to the agent's
// own tool timeout: the phone's ten minutes < the manager < the daemon's action bound < the
// control call < Codex's tool_timeout_sec for this server.
func TestAHandoffOutlivesEveryLayerAroundIt(t *testing.T) {
	handoff := phone.AgentAction{Action: phone.ActHandoff}
	control := handoff.Budget() + phoneCallMargin
	if handoff.Budget() <= phone.HandoffDefault || control <= handoff.Budget() {
		t.Fatalf("budget %v, control call %v", handoff.Budget(), control)
	}
	if codex := time.Duration(work.PhoneToolTimeoutSec) * time.Second; codex <= control {
		t.Fatalf("Codex gives up after %v, before the control call's %v", codex, control)
	}
	// The usual bound of the computer tool, 45 seconds, would have cut a handoff short.
	if handoff.Budget() < 10*time.Minute {
		t.Fatalf("a handoff gets %v", handoff.Budget())
	}
}

func TestTurningPhoneUseOffReleasesThePhone(t *testing.T) {
	root := t.TempDir()
	agents, _ := agentuse.New(root, false)
	manager := phone.NewManager(phone.Options{})
	device := &answeringPhone{handle: func(r phone.Request) *phone.Result { return &phone.Result{OK: true} }}
	link, _ := manager.Attach(phone.Hello{Type: phone.TypeHello, Version: phone.Version, State: phone.StateReady}, device)
	device.link = link
	app := &controlApp{phone: phone.NewAgent(manager, nil), agentUse: agents}
	if resp := app.HandleControlRequest(control.Request{Type: "phone", WorkerID: "%7", PhoneAction: &phone.AgentAction{Action: phone.ActScreen}}); resp.OK || resp.Error.Code != "phone_off" {
		t.Fatalf("%+v", resp)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(device.ops()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if ops := device.ops(); len(ops) != 1 || ops[0] != phone.OpRelease {
		t.Fatalf("ops %v", ops)
	}
	if resp := (&controlApp{}).HandleControlRequest(control.Request{Type: "phone", WorkerID: "%7"}); resp.OK || resp.Error.Code != "phone_unavailable" {
		t.Fatalf("%+v", resp)
	}
}
