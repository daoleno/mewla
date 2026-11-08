package agentuse

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/daoleno/mewla/daemon/screen"
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
	if chromeBin == "" || mcpCLI == "" || mewla == "" || (runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "") {
		t.Skip("set MEWLA_E2E_CHROME, MEWLA_E2E_MCP and MEWLA_E2E_BIN (and DISPLAY on Linux)")
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
	var inflight, maxInflight atomic.Int32
	var recMu sync.Mutex
	records := map[string][]string{}
	var typed atomic.Value
	typed.Store("")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/record":
			hits.Add(1)
			typed.Store(r.URL.Query().Get("v"))
			io.WriteString(w, "ok")
		case "/record2":
			// Workers' pages record who sent what; a short wait makes
			// concurrent requests overlap, and the overlap is counted.
			n := inflight.Add(1)
			for {
				old := maxInflight.Load()
				if n <= old || maxInflight.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(120 * time.Millisecond)
			inflight.Add(-1)
			recMu.Lock()
			who := r.URL.Query().Get("w")
			records[who] = append(records[who], r.URL.Query().Get("v"))
			recMu.Unlock()
			io.WriteString(w, "ok")
		case "/person":
			io.WriteString(w, `<title>person</title><input id=p autofocus><body>the person's page`)
		default:
			if strings.HasPrefix(r.URL.Path, "/w/") {
				name := strings.TrimPrefix(r.URL.Path, "/w/")
				fmt.Fprintf(w, `<title>page of %s</title><input aria-label="Note"><button onclick="fetch('/record2?w=%s&v='+encodeURIComponent(document.querySelector('input').value)).then(()=>document.title='sent '+document.querySelector('input').value)">Send</button>`, name, name)
				return
			}
			io.WriteString(w, `<title>agent page</title><input id=name aria-label="Name"><button onclick="fetch('/record?v='+encodeURIComponent(document.getElementById('name').value)).then(()=>document.title='sent')">Send</button>`)
		}
	}))
	defer site.Close()

	svc, err := New(state, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.lookChrome = func(string) (chromeInstall, bool) { return chromeInstall{chromeBin, profile}, true }
	svc.lookDesktop = func() ([]string, bool) {
		if runtime.GOOS == "linux" {
			return []string{"DISPLAY=" + os.Getenv("DISPLAY")}, true
		}
		return nil, true
	}
	// On a Mac, with MEWLA_E2E_AGENT_DISPLAY=1, the agent's window opens on the
	// agent display, as the daemon does for it.
	var displayBounds *WindowBounds
	if os.Getenv("MEWLA_E2E_AGENT_DISPLAY") == "1" {
		hold, release, ok := screen.NewService(root).AgentDisplay()
		if !ok {
			t.Fatal("this computer has no agent display")
		}
		svc.SetAgentDisplay(func(key string) (WindowBounds, error) {
			r, err := hold(key)
			b := WindowBounds{Left: r.Min.X, Top: r.Min.Y, Width: r.Dx(), Height: r.Dy()}
			displayBounds = &b
			return b, err
		}, release)
	}
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
	go func() {
		if err := bridge.Serve(ctx); err != nil {
			t.Errorf("the bridge can't listen: %v", err)
		}
	}()

	// The person's Chrome, with a window of their own on a page.
	chromeArgs := []string{"--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port=9334", "--load-extension=" + extDir, "--window-size=900,700", site.URL + "/person"}
	chrome := exec.Command(chromeBin, chromeArgs...)
	if runtime.GOOS == "linux" {
		chrome.Args = append(chrome.Args, "--no-sandbox")
		chrome.Env = append(os.Environ(), "HOME="+root)
		chrome.Stderr = os.Stderr
	}
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
		sock := bridgeSocket(filepath.Join(state, "agent-use"))
		_, sockErr := os.Stat(sock)
		_, manifestErr := os.Stat(filepath.Join(profile, "NativeMessagingHosts", NativeHostName+".json"))
		t.Logf("bridge socket %s (%d bytes): %v; host manifest: %v", sock, len(sock), sockErr, manifestErr)
		macDiagnostics(t)
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
	if !strings.Contains(person, `"title":"person"`) {
		// Chrome never loaded the page this test serves on 127.0.0.1: nothing
		// that Chrome opens to this computer's loopback is going to work.
		macDiagnostics(t)
		t.Fatalf("Chrome could not load the test page %s/person (the person's window stayed blank)", site.URL)
	}
	if runtime.GOOS == "darwin" {
		// Chrome, started by this test, came to the front as any app does when
		// it starts. The person is using something else: put Finder in front, so
		// "Chrome never comes to the front" and "the front app is unchanged"
		// mean something.
		_ = exec.Command("/usr/bin/open", "-b", "com.apple.finder").Run()
		time.Sleep(time.Second)
	}
	frontBefore := frontAppPID()
	t.Logf("app in front before: %d (Chrome is %d)", frontBefore, chrome.Process.Pid)

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

	if displayBounds != nil {
		got := agentWindowBounds(t)
		t.Logf("agent window at %+v, agent display %+v", got, *displayBounds)
		if got.Left < displayBounds.Left || got.Left >= displayBounds.Left+displayBounds.Width {
			t.Fatalf("the agent's window is not on the agent display: %+v", got)
		}
	}
	if frontAfter := frontAppPID(); frontAfter != frontBefore {
		t.Fatalf("the app in front changed from %d to %d", frontBefore, frontAfter)
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

	// Several Workers at once in this same Chrome: each in a window of its own
	// and a tab group with its name; all of them get on at the same time, and
	// what each does reaches only its own page.
	svc.Bridge().WorkerName = func(id string) string {
		return map[string]string{"%e2e1": "Alpha", "%e2e2": "Beta", "%e2e3": "Gamma"}[id]
	}
	workers := []string{"%e2e1", "%e2e2", "%e2e3"}
	names := map[string]string{"%e2e1": "Alpha", "%e2e2": "Beta", "%e2e3": "Gamma"}
	procs := map[string]*mcpProc{}
	for _, id := range workers {
		procs[id] = startMCP(t, argv, append(append([]string(nil), env...), "MEWLA_WORKER_ID="+id))
		defer procs[id].close()
	}
	const rounds = 5
	runAll := time.Now()
	var wg sync.WaitGroup
	failures := make(chan error, len(workers))
	barrier := make(chan struct{})
	for _, id := range workers {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, name := procs[id], names[id]
			<-barrier
			if out, err := p.callE("browser_navigate", map[string]any{"url": site.URL + "/w/" + name}); err != nil || !strings.Contains(out, "page of "+name) {
				failures <- fmt.Errorf("%s navigate: %v %s", name, err, out)
				return
			}
			for i := 1; i <= rounds; i++ {
				script := fmt.Sprintf(`async (page) => {
				  await page.getByLabel('Note').fill('%s-%d');
				  await page.getByRole('button', {name: 'Send'}).click();
				  await page.waitForFunction(() => document.title === 'sent %s-%d');
				  return await page.title();
				}`, name, i, name, i)
				if out, err := p.callE("browser_run_code_unsafe", map[string]any{"code": script}); err != nil {
					failures <- fmt.Errorf("%s round %d: %v %s", name, i, err, out)
					return
				}
			}
		}()
	}
	close(barrier)
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(120 * time.Second):
		t.Fatal("the Workers did not finish: they block each other")
	}
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	t.Logf("%d Workers, %d rounds each, at the same time: %s (overlapping requests: %d)", len(workers), rounds, time.Since(runAll).Round(10*time.Millisecond), maxInflight.Load())
	recMu.Lock()
	for _, id := range workers {
		name := names[id]
		var want []string
		for i := 1; i <= rounds; i++ {
			want = append(want, fmt.Sprintf("%s-%d", name, i))
		}
		if strings.Join(records[name], ",") != strings.Join(want, ",") {
			t.Errorf("%s's page got %v, want %v", name, records[name], want)
		}
	}
	recMu.Unlock()
	if maxInflight.Load() < 2 {
		t.Errorf("the Workers never overlapped (%d at most): they ran one after another", maxInflight.Load())
	}
	if n := countWindows(t); n != 1+len(workers) {
		t.Errorf("expected the person's window and %d Workers' windows, got %d", len(workers), n)
	}
	if now := observePersonWindow(t); now != person {
		t.Errorf("the person's window changed while the Workers worked:\n before %s\n after  %s", person, now)
	}
	t.Logf("windows: %s", extensionEval(t, `chrome.windows.getAll({}).then(ws => JSON.stringify(ws.map(w => ({id: w.id, focused: w.focused, state: w.state, left: w.left}))))`))
	groups := tabGroups(t)
	t.Logf("tab groups: %s", groups)
	for _, name := range names {
		if strings.Count(groups, `"title":"`+name+`"`) != 1 {
			t.Errorf("expected one tab group named %s in %s", name, groups)
		}
	}
	if frontAfter := frontAppPID(); frontAfter != frontBefore {
		t.Errorf("the app in front changed from %d to %d", frontBefore, frontAfter)
	}
	for _, id := range workers {
		procs[id].close()
	}
	for i := 0; i < 50 && countWindows(t) != 1; i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if n := countWindows(t); n != 1 {
		t.Errorf("every Worker's window should close with its tools; %d windows remain", n)
	}

	// How real Workers end: each runs under the supervisor, closes its browser
	// itself (browser_close) and is then closed, which makes the supervisor
	// close "its tabs" once more. Nothing may open a window then, and none may
	// be left behind: only the person's window remains, now and later.
	var supervised []*supervisedProc
	for _, id := range workers {
		supervised = append(supervised, startSupervised(t, argv, append(append([]string(nil), env...), "MEWLA_WORKER_ID="+id)))
	}
	for i, p := range supervised {
		if out, err := p.callE("browser_navigate", map[string]any{"url": site.URL + "/w/" + names[workers[i]]}); err != nil {
			t.Fatalf("%s navigate: %v %s", names[workers[i]], err, out)
		}
	}
	if n := countWindows(t); n != 1+len(workers) {
		t.Errorf("expected %d windows with the Workers working, got %d", 1+len(workers), n)
	}
	for i, p := range supervised {
		if out, err := p.callE("browser_close", map[string]any{}); err != nil {
			t.Fatalf("%s browser_close: %v %s", names[workers[i]], err, out)
		}
	}
	for _, p := range supervised {
		p.end()
	}
	// The windows close; then, for a while after, nothing may open one again.
	for i := 0; i < 75 && countWindows(t) != 1; i++ {
		time.Sleep(200 * time.Millisecond)
	}
	for i := 0; i < 40; i++ {
		if n := countWindows(t); n != 1 {
			t.Errorf("after the Workers ended there should be only the person's window, got %d: %s", n,
				extensionEval(t, `chrome.windows.getAll({populate: true}).then(ws => JSON.stringify(ws.map(w => ({id: w.id, left: w.left, width: w.width, tabs: (w.tabs||[]).map(x => x.url)}))))`))
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if now := observePersonWindow(t); now != person {
		t.Errorf("the person's window changed over the Workers' endings:\n before %s\n after  %s", person, now)
	}
}

// supervisedProc is a Worker's browser tools as a real Worker runs them: under
// SuperviseMCP, which closes the agent's tabs when its stdin ends.
type supervisedProc struct {
	*mcpProc
	done chan struct{}
}

func startSupervised(t *testing.T, argv, env []string) *supervisedProc {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer outW.Close()
		_ = SuperviseMCP(argv, env, nil, inR, outW, os.Stderr)
	}()
	p := &mcpProc{cmd: &exec.Cmd{}, in: inW, lines: make(chan string, 64)}
	go func() {
		s := bufio.NewScanner(outR)
		s.Buffer(make([]byte, 1<<20), 16<<20)
		for s.Scan() {
			p.lines <- s.Text()
		}
		close(p.lines)
	}()
	p.rpc(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "e2e", "version": "1"}})
	fmt.Fprintln(inW, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return &supervisedProc{mcpProc: p, done: done}
}

// end is the Worker being closed: its stdin ends and the supervisor cleans up.
func (p *supervisedProc) end() {
	p.in.Close()
	select {
	case <-p.done:
	case <-time.After(30 * time.Second):
	}
}

// tabGroups is Chrome's tab groups as the extension sees them.
func tabGroups(t *testing.T) string {
	t.Helper()
	return extensionEval(t, `chrome.tabGroups.query({}).then(g => JSON.stringify(g.map(x => ({title: x.title, windowId: x.windowId, color: x.color}))))`)
}

// extensionEval runs an expression in the extension's service worker.
func extensionEval(t *testing.T, expr string) string {
	t.Helper()
	for _, target := range cdpTargets(t) {
		if target["type"] == "service_worker" && strings.HasPrefix(fmt.Sprint(target["url"]), "chrome-extension://"+MewlaExtensionID+"/") {
			return evalAwaitIn(t, fmt.Sprint(target["webSocketDebuggerUrl"]), expr)
		}
	}
	t.Fatal("the extension's service worker isn't running")
	return ""
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

// callE is call for goroutines: it returns the failure instead of ending the
// test.
func (p *mcpProc) callE(name string, args map[string]any) (string, error) {
	p.id++
	id := p.id
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	fmt.Fprintln(p.in, string(raw))
	deadline := time.After(90 * time.Second)
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				return "", errors.New("the browser tools ended")
			}
			var msg struct {
				ID     int `json:"id"`
				Result struct {
					Content []struct{ Text string } `json:"content"`
					IsError bool                    `json:"isError"`
				} `json:"result"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(line), &msg) != nil || msg.ID != id {
				continue
			}
			if msg.Error != nil {
				return "", fmt.Errorf("%s: %s", name, msg.Error)
			}
			var texts []string
			for _, c := range msg.Result.Content {
				texts = append(texts, c.Text)
			}
			out := strings.Join(texts, "\n")
			if msg.Result.IsError {
				return out, fmt.Errorf("%s failed: %s", name, out)
			}
			return out, nil
		case <-deadline:
			return "", fmt.Errorf("%s: no answer in 90 s", name)
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
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		} `json:"result"`
	}
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Result.ExceptionDetails != nil {
		t.Fatalf("%s: %s", expr, reply.Result.ExceptionDetails)
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

// frontAppPID is the pid of the app in front on a Mac, 0 elsewhere.
func frontAppPID() int {
	if runtime.GOOS != "darwin" {
		return 0
	}
	front, err := exec.Command("/usr/bin/lsappinfo", "front").Output()
	if err != nil {
		return -1
	}
	out, err := exec.Command("/usr/bin/lsappinfo", "info", "-only", "pid", strings.TrimSpace(string(front))).Output()
	if err != nil {
		return -1
	}
	// `"pid"=123` on some releases, `"pid" = 123` on others.
	digits := regexp.MustCompile(`\d+`).FindAllString(string(out), -1)
	if len(digits) == 0 {
		return -1
	}
	pid, _ := strconv.Atoi(digits[len(digits)-1])
	return pid
}

// agentWindowBounds is where the agent's page's window is, from Chrome.
func agentWindowBounds(t *testing.T) WindowBounds {
	t.Helper()
	resp, err := http.Get("http://127.0.0.1:9334/json/version")
	if err != nil {
		t.Fatal(err)
	}
	var version struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&version)
	resp.Body.Close()
	var targetID string
	for _, target := range cdpTargets(t) {
		u := fmt.Sprint(target["url"])
		if target["type"] == "page" && !strings.HasSuffix(u, "/person") && !strings.HasPrefix(u, "chrome-extension://") {
			targetID = fmt.Sprint(target["id"])
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(version.WS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"id": 1, "method": "Browser.getWindowForTarget", "params": map[string]any{"targetId": targetID}})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var reply struct {
		Result struct {
			Bounds WindowBounds `json:"bounds"`
		} `json:"result"`
	}
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	return reply.Result.Bounds
}

// macDiagnostics logs what on a Mac can keep Chrome from reaching a program
// that listens on 127.0.0.1 (the application firewall asks about, and holds
// connections to, programs it doesn't know): read-only.
func macDiagnostics(t *testing.T) {
	if runtime.GOOS != "darwin" {
		return
	}
	for _, args := range [][]string{
		{"/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate"},
		{"/usr/libexec/ApplicationFirewall/socketfilterfw", "--getblockall"},
		{"/usr/libexec/ApplicationFirewall/socketfilterfw", "--getallowsigned"},
		{"/usr/bin/lsappinfo", "front"},
		{"/usr/bin/log", "show", "--last", "2m", "--style", "compact", "--predicate", `process == "socketfilterfw" OR subsystem == "com.apple.alf"`},
	} {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		text := strings.TrimSpace(string(out))
		if lines := strings.Split(text, "\n"); len(lines) > 40 {
			text = strings.Join(lines[len(lines)-40:], "\n")
		}
		t.Logf("%s: %s %v", strings.Join(args, " "), text, err)
	}
}

// evalAwaitIn is evalIn for an expression that gives a promise.
func evalAwaitIn(t *testing.T, wsURL, expr string) string {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var raw json.RawMessage
	for {
		var msg struct {
			ID int `json:"id"`
		}
		if err := conn.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		// A worker target announces itself before it answers.
		if json.Unmarshal(raw, &msg) == nil && msg.ID == 1 {
			break
		}
	}
	var reply struct {
		Result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &reply)
	if reply.Result.ExceptionDetails != nil {
		t.Fatalf("%s: %s", expr, reply.Result.ExceptionDetails)
	}
	return reply.Result.Result.Value
}
