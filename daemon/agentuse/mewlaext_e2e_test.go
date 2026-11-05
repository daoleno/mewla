package agentuse

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"time"
)

// The whole chain on this computer, with the real Playwright MCP server and a
// real Chrome for Testing carrying Mewla's extension: the browser tools ask
// Chrome to open, Mewla's shim has the extension connect through the native
// host and the daemon's bridge, and an agent's tabs open in a window of
// their own while a "person's" window in the same Chrome stays as it was.
//
//	DISPLAY=:98 MEWLA_E2E_CHROME=<chrome> MEWLA_E2E_MCP=<@playwright/mcp/cli.js> \
//	  MEWLA_E2E_BIN=<mewla binary> go test ./agentuse -run MewlaExtensionE2E -v -count=1
func TestMewlaExtensionE2E(t *testing.T) {
	chromeBin, mcpCLI, mewla := os.Getenv("MEWLA_E2E_CHROME"), os.Getenv("MEWLA_E2E_MCP"), os.Getenv("MEWLA_E2E_BIN")
	if chromeBin == "" || mcpCLI == "" || mewla == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("set DISPLAY, MEWLA_E2E_CHROME, MEWLA_E2E_MCP and MEWLA_E2E_BIN")
	}
	root, err := os.MkdirTemp("", "e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	state := filepath.Join(root, "state")
	defer func() {
		if raw, err := os.ReadFile(filepath.Join(state, "agent-use", "open.log")); err == nil {
			t.Logf("open.log:\n%s", raw)
		}
	}()
	profile := filepath.Join(root, "chrome")

	// A page the agent drives, and one the person is reading.
	var hits atomic.Int32
	var typed atomic.Value
	typed.Store("")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/record":
			hits.Add(1)
			typed.Store(r.URL.Query().Get("v"))
			io.WriteString(w, "ok")
		case "/person":
			io.WriteString(w, `<title>person</title><input id=p autofocus><body>the person's page`)
		default:
			io.WriteString(w, `<title>agent page</title><input id=name aria-label="Name"><button onclick="fetch('/record?v='+encodeURIComponent(document.getElementById('name').value)).then(()=>document.title='sent')">Send</button>`)
		}
	}))
	defer site.Close()

	svc, err := New(state, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.lookChrome = func(string) (chromeInstall, bool) { return chromeInstall{chromeBin, profile}, true }
	svc.lookDesktop = func() ([]string, bool) { return []string{"DISPLAY=" + os.Getenv("DISPLAY")}, true }
	svc.executable = func() (string, error) { return mewla, nil }
	extDir, err := svc.ExtensionDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.InstallNativeHost(profile, mewla, []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}
	// The browser tools, from an existing install of the pinned version.
	pkg := svc.packageDir()
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(filepath.Dir(filepath.Dir(mcpCLI))), filepath.Join(pkg, "node_modules")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pkg, ".mewla-installed"), []byte(lockStamp()+"\n"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge := svc.Bridge()
	go bridge.Serve(ctx)

	// The person's Chrome, with a window of their own on a page.
	chrome := exec.Command(chromeBin, "--user-data-dir="+profile, "--no-sandbox", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port=9334", "--load-extension="+extDir, "--window-size=900,700", site.URL+"/person")
	chrome.Env = append(os.Environ(), "HOME="+root)
	chrome.Stderr = os.Stderr
	if err := chrome.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chrome.Process.Kill(); _ = chrome.Wait() }()
	for i := 0; i < 100 && !bridge.HasHost(); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if !bridge.HasHost() {
		for _, target := range cdpTargets(t) {
			t.Logf("target %v %v", target["type"], target["url"])
		}
		t.Fatal("Mewla's extension never connected through its native host")
	}
	t.Logf("extension connected, version %s", bridge.HostVersion())

	// An unpacked extension has no folder under Extensions the way an
	// installed one does; stand in for it so the status sees it.
	if err := os.MkdirAll(filepath.Join(profile, "Default", "Extensions", MewlaExtensionID), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if strings.Contains(observePersonWindow(t), `"title":"person"`) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	person := observePersonWindow(t)
	t.Logf("person's window before: %s", person)

	status := svc.Refresh()
	if status.Browser.State != BrowserReady {
		t.Fatalf("browser status %+v", status.Browser)
	}
	argv, env, err := svc.MCPServer()
	if err != nil {
		t.Fatal(err)
	}
	argv[1] = mcpCLI
	if !strings.Contains(strings.Join(argv, " "), "chrome-for-tools.sh") {
		t.Fatalf("the tools weren't pointed at Mewla's stand-in for Chrome: %v", argv)
	}
	mcp := startMCP(t, argv, env)
	defer mcp.close()

	start := time.Now()
	out := mcp.call(t, "browser_navigate", map[string]any{"url": site.URL + "/"})
	t.Logf("navigate (%s): %s", time.Since(start).Round(time.Millisecond), firstLine(out))
	if !strings.Contains(out, "agent page") {
		t.Fatalf("navigate: %s", out)
	}
	t.Logf("person's window after navigate: %s", observePersonWindow(t))
	if dir := os.Getenv("MEWLA_E2E_SHOTS"); dir != "" {
		shotOfExtensionPage(t, filepath.Join(dir, "popup-agent-working.png"))
	}
	script := `async (page) => {
	  await page.getByLabel('Name').fill('hello from the agent');
	  await page.getByRole('button', {name: 'Send'}).click();
	  await page.waitForFunction(() => document.title === 'sent');
	  return await page.title();
	}`
	start = time.Now()
	out = mcp.call(t, "browser_run_code_unsafe", map[string]any{"code": script})
	t.Logf("fill + click (%s): %s", time.Since(start).Round(time.Millisecond), firstLine(out))
	if hits.Load() != 1 || typed.Load().(string) != "hello from the agent" {
		t.Fatalf("the server saw %d hits, value %q: %s", hits.Load(), typed.Load(), out)
	}

	after := observePersonWindow(t)
	t.Logf("person's window after: %s", after)
	if after != person {
		t.Fatalf("the person's window changed:\n before %s\n after  %s", person, after)
	}
	agentWindows := countWindows(t)
	if agentWindows != 2 {
		t.Fatalf("expected the person's window and the agent's, got %d windows", agentWindows)
	}
	mcp.close()
	for i := 0; i < 50 && countWindows(t) != 1; i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if n := countWindows(t); n != 1 {
		t.Fatalf("the agent's window should close with the tools; %d windows remain", n)
	}
}

type mcpProc struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan string
	id    int
}

func startMCP(t *testing.T, argv, env []string) *mcpProc {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &mcpProc{cmd: cmd, in: in, lines: make(chan string, 64)}
	go func() {
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 1<<20), 16<<20)
		for s.Scan() {
			p.lines <- s.Text()
		}
		close(p.lines)
	}()
	p.rpc(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "e2e", "version": "1"}})
	fmt.Fprintln(in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return p
}

func (p *mcpProc) rpc(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	p.id++
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": p.id, "method": method, "params": params})
	fmt.Fprintln(p.in, string(raw))
	deadline := time.After(90 * time.Second)
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				t.Fatal("the browser tools ended")
			}
			var msg struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(line), &msg) == nil && msg.ID == p.id {
				if msg.Error != nil {
					t.Fatalf("%s: %s", method, msg.Error)
				}
				return msg.Result
			}
		case <-deadline:
			t.Fatalf("%s: no answer in 90 s", method)
		}
	}
}

func (p *mcpProc) call(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	res := p.rpc(t, "tools/call", map[string]any{"name": name, "arguments": args})
	var r struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	_ = json.Unmarshal(res, &r)
	var texts []string
	for _, c := range r.Content {
		texts = append(texts, c.Text)
	}
	out := strings.Join(texts, "\n")
	if r.IsError {
		t.Fatalf("%s failed: %s", name, out)
	}
	return out
}

func (p *mcpProc) close() {
	if p.cmd.Process == nil {
		return
	}
	p.in.Close()
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
	}
	p.cmd.Process = nil
}

// cdpTargets lists the observer's view of Chrome's pages.
func cdpTargets(t *testing.T) []map[string]any {
	t.Helper()
	resp, err := http.Get("http://127.0.0.1:9334/json/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var targets []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&targets)
	return targets
}

// observePersonWindow describes the person's page: its title, URL and whether
// it still has focus and is visible, read by a script the page runs.
func observePersonWindow(t *testing.T) string {
	t.Helper()
	for _, target := range cdpTargets(t) {
		if target["type"] == "page" && strings.HasSuffix(fmt.Sprint(target["url"]), "/person") {
			return evalIn(t, fmt.Sprint(target["webSocketDebuggerUrl"]), `JSON.stringify({title: document.title, url: location.pathname, focus: document.hasFocus(), visible: document.visibilityState, active: document.activeElement && document.activeElement.id})`)
		}
	}
	t.Fatal("the person's page is gone")
	return ""
}

func countWindows(t *testing.T) int {
	t.Helper()
	pages := 0
	for _, target := range cdpTargets(t) {
		if target["type"] == "page" && !strings.HasPrefix(fmt.Sprint(target["url"]), "chrome-extension://") {
			pages++
		}
	}
	// One page per window here: the person's and the agent's.
	return pages
}

// evalIn runs an expression in a page over the observer's DevTools port.
func evalIn(t *testing.T, wsURL, expr string) string {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expr, "returnByValue": true}})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var reply struct {
		Result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	return reply.Result.Result.Value
}

// shotOfExtensionPage saves a 1280x800 screenshot of the extension's status
// page, for the store listing. It runs in a tab of the observer's own, closed
// after.
func shotOfExtensionPage(t *testing.T, path string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:9334/json/new?chrome-extension://"+MewlaExtensionID+"/status.html", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var target struct {
		ID string `json:"id"`
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&target)
	resp.Body.Close()
	defer http.Get("http://127.0.0.1:9334/json/close/" + target.ID)
	conn, _, err := websocket.DefaultDialer.Dial(target.WS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	call := func(id int, method string, params map[string]any) json.RawMessage {
		_ = conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
		for {
			var m struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err := conn.ReadJSON(&m); err != nil {
				t.Fatal(err)
			}
			if m.ID == id {
				return m.Result
			}
		}
	}
	// The page is the popup's size; a 4x capture of 320x200 is 1280x800.
	call(1, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 320, "height": 200, "deviceScaleFactor": 1, "mobile": false})
	time.Sleep(500 * time.Millisecond)
	var shot struct {
		Data string `json:"data"`
	}
	_ = json.Unmarshal(call(3, "Page.captureScreenshot", map[string]any{"format": "png", "clip": map[string]any{"x": 0, "y": 0, "width": 320, "height": 200, "scale": 4}}), &shot)
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil || len(raw) == 0 {
		t.Fatalf("screenshot: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
