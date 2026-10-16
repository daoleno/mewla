package main

import (
	"bytes"
	"context"
	"encoding/json"
	stdimage "image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestComputerMCPHelper(t *testing.T) {
	if os.Getenv("MEWLA_COMPUTER_MCP_FIXTURE") != "1" {
		return
	}
	if err := runAgentUseCommand([]string{"computer-mcp", "--state-dir", os.Getenv("MEWLA_COMPUTER_FIXTURE_STATE")}, os.Stderr); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

// A real MCP client launches the real `mewla agent-use computer-mcp`, which
// reaches the daemon's handler over the real control socket. Only the
// display (a Retina-sized fake) and Worker liveness are stand-ins.
func TestComputerUseThroughTheControlSocket(t *testing.T) {
	root := t.TempDir()
	backend := screen.NewFakeBackend(2560, 1600)
	screens := screen.NewServiceWith(root, backend)
	defer screens.StopAgent()
	agents, err := agentuse.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agents.SetComputer(agentuse.ComputerHooks{
		Status:       func() agentuse.ComputerStatus { return computerStatus(ctx, screens) },
		OpenSettings: func(p string) error { return screens.OpenAgentSettings(ctx, p) },
		Stop:         screens.StopAgent,
	})
	watcher := newFakeControlWatcher()
	watcher.workers["%7"] = &classifier.Worker{ID: "%7", Name: "reply-writer", State: classifier.StateRunning}
	app := &controlApp{screen: screens, agentUse: agents, watcher: watcher}
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
		cmd := exec.Command(exe, "-test.run=^TestComputerMCPHelper$")
		cmd.Env = append(os.Environ(), "MEWLA_COMPUTER_MCP_FIXTURE=1", "MEWLA_COMPUTER_FIXTURE_STATE="+root, "MEWLA_WORKER_ID="+worker)
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-agent", Version: "1"}, nil)
		session, e := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	session := attach("%7")
	defer session.Close()
	call := func(s *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, string) {
		t.Helper()
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "computer", Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(r)
		return r, string(raw)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("tools %+v %v", tools, err)
	}

	shot, raw := call(session, map[string]any{"action": "screenshot"})
	if shot.IsError || strings.Contains(raw, "structuredContent") {
		t.Fatalf("screenshot: %s", raw[:min(len(raw), 300)])
	}
	image, ok := shot.Content[0].(*mcp.ImageContent)
	if !ok || image.MIMEType != "image/jpeg" {
		t.Fatalf("screenshot content %T", shot.Content[0])
	}
	decoded, err := jpeg.Decode(bytes.NewReader(image.Data))
	if err != nil || decoded.Bounds().Dx() != 1280 || decoded.Bounds().Dy() != 800 {
		t.Fatalf("screenshot %v %v", decoded.Bounds(), err)
	}
	if status := agents.Status(); status.Computer == nil || status.Computer.Active == nil || status.Computer.Active.Worker != "%7" {
		t.Fatalf("the app does not see the agent: %+v", status.Computer)
	}

	call(session, map[string]any{"action": "left_click", "coordinate": []int{640, 400}})
	call(session, map[string]any{"action": "type", "text": "收到"})
	pointers, keys := backend.Snapshot()
	if p := pointers[len(pointers)-1]; p != (screen.FakePointer{X: 1280, Y: 800, Buttons: 0}) {
		t.Fatalf("click landed at %v", p)
	}
	if k := keys[len(keys)-2]; k.Keysym != 0x01000000+'到' || !k.Down {
		t.Fatalf("typed %v", keys)
	}

	// The focused window, as elements with points to click.
	backend.Tree = &screen.ElementTree{App: "Notes", Elements: []screen.Element{
		{Role: "AXButton", Title: "New Note", Frame: stdimage.Rect(100, 40, 300, 80), Enabled: true},
	}}
	if r, raw := call(session, map[string]any{"action": "elements"}); r.IsError || !strings.Contains(raw, `button \"New Note\" at (100, 30)`) {
		t.Fatalf("elements: %s", raw[:min(len(raw), 400)])
	}

	// Any app, by name, without bringing it forward.
	backend.RunningApps = []screen.FakeApp{{App: screen.App{Name: "Mail", PID: 44}, Elements: []screen.Element{
		{Role: "AXButton", Title: "Send", Enabled: true, Actions: []string{"AXPress"}},
	}}}
	if r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "apps", Arguments: map[string]any{}}); err != nil || r.IsError || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "Mail, pid 44\n") {
		t.Fatalf("apps: %+v %v", r, err)
	}
	backend.InstalledApps = []screen.FakeInstalledApp{{FakeApp: screen.FakeApp{App: screen.App{Name: "Calendar", PID: 66}}}}
	if r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "open_app", Arguments: map[string]any{"app": "Calendar"}}); err != nil || r.IsError || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "Opened Calendar (pid 66) behind the app in front") {
		t.Fatalf("open_app: %+v %v", r, err)
	}
	if r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "app_state", Arguments: map[string]any{"app": "mail"}}); err != nil || r.IsError || !strings.Contains(r.Content[0].(*mcp.TextContent).Text, `[1] Button "Send" {Press}`) {
		t.Fatalf("app_state: %+v %v", r, err)
	}
	if r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "element", Arguments: map[string]any{"id": 1, "action": "press"}}); err != nil || r.IsError || len(backend.ElementActions) != 1 || backend.ElementActions[0].Action != "AXPress" {
		t.Fatalf("element: %+v %v %+v", r, err, backend.ElementActions)
	}
	steps := []map[string]any{
		{"action": "app_state", "app": "Mail"},
		{"action": "element", "match": map[string]any{"role": "button", "title": "send"}, "element_action": "press"},
		{"action": "expect", "match": map[string]any{"title": "Send"}},
		// Named the way the element tool names them.
		{"action": "element", "id": 1, "element_action": "set_value", "value": "x"},
	}
	if r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "steps", Arguments: map[string]any{"steps": steps}}); err != nil || r.IsError || len(backend.ElementActions) != 3 || backend.ElementActions[2].Value != "x" {
		t.Fatalf("steps: %+v %v %+v", r, err, backend.ElementActions)
	}

	// A person opens Screen: the agent is refused until they are done.
	_, personCancel := context.WithCancelCause(context.Background())
	person := screens.TakeControl(personCancel)
	if r, raw := call(session, map[string]any{"action": "screenshot"}); !r.IsError || !strings.Contains(raw, "person") {
		t.Fatalf("agent acted while a person was in control: %s", raw[:min(len(raw), 300)])
	}
	screens.ReleaseControl(person)

	// The app's switch, and Screen's own kill switch, stop every action, and
	// turning agents off ends the one on the screen at once.
	call(session, map[string]any{"action": "screenshot"})
	if _, ok := screens.Agent(); !ok {
		t.Fatal("the agent is not on the screen")
	}
	if _, err := agents.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := screens.Agent(); ok {
		t.Fatal("turning agents off left the agent on the screen")
	}
	if r, raw := call(session, map[string]any{"action": "screenshot"}); !r.IsError || !strings.Contains(raw, "turned off in the app") {
		t.Fatalf("switch off: %s", raw[:min(len(raw), 300)])
	}
	if _, err := agents.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := screen.SaveConfig(root, screen.Config{Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if r, raw := call(session, map[string]any{"action": "screenshot"}); !r.IsError || !strings.Contains(raw, "turned off") {
		t.Fatalf("Screen off: %s", raw[:min(len(raw), 300)])
	}
	if status := agents.Status(); status.Computer.State != agentuse.ComputerScreenOff {
		t.Fatalf("status with Screen off: %+v", status.Computer)
	}
	if err := screen.SaveConfig(root, screen.Config{}); err != nil {
		t.Fatal(err)
	}

	// A locked screen: the agent hears why, the app says so, and agents
	// started meanwhile still get the tool for when it is unlocked.
	backend.SetLocked(true)
	if r, raw := call(session, map[string]any{"action": "screenshot"}); !r.IsError || !strings.Contains(raw, "locked") {
		t.Fatalf("locked: %s", raw[:min(len(raw), 300)])
	}
	if status := agents.Status(); status.Computer.State != agentuse.ComputerLocked || !agents.ComputerReady() {
		t.Fatalf("status while locked: %+v", status.Computer)
	}
	backend.SetLocked(false)

	// Only a running Mewla Worker may act.
	ghost := attach("%99")
	defer ghost.Close()
	if r, raw := call(ghost, map[string]any{"action": "screenshot"}); !r.IsError || !strings.Contains(raw, "running Mewla Worker") {
		t.Fatalf("unknown worker: %s", raw[:min(len(raw), 300)])
	}

	audit, err := os.ReadFile(filepath.Join(root, "screen-agent-audit.jsonl"))
	if err != nil || strings.Contains(string(audit), "收到") || !strings.Contains(string(audit), `"worker":"%7"`) {
		t.Fatalf("audit %v: %s", err, audit)
	}
}
