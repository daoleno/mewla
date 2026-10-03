//go:build linux && browserfixture

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/browser"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	zenserver "github.com/daoleno/zen/daemon/server"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBrowserMCPHelper(t *testing.T) {
	if os.Getenv("ZEN_BROWSER_MCP_FIXTURE") != "1" {
		return
	}
	err := runBrowserCommand([]string{"mcp", "--id", os.Getenv("ZEN_BROWSER_FIXTURE_ID"), "--task", "fixture-task", "--state-dir", os.Getenv("ZEN_BROWSER_FIXTURE_STATE")}, os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestBrowserFirstProductFlow(t *testing.T) {
	if os.Getenv("ZEN_BROWSER_INTEGRATION") != "1" {
		t.Skip("opt-in isolated graphical browser")
	}
	evidence := os.Getenv("ZEN_BROWSER_EVIDENCE")
	root := t.TempDir()
	backend := browser.NewFixtureBackend()
	if os.Getenv("ZEN_BROWSER_SECURE") == "1" {
		backend = browser.NewBackend()
	}
	m, err := browser.New(filepath.Join(root, "browsers"), backend)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, err := auth.NewManager(filepath.Join(root, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pair, _ := a.IssuePairingToken(time.Minute)
	_, err = a.EnrollDevice(pair.Value, a.DaemonID(), a.PublicKeyHex(), "fixture-phone", "Fixture", hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	s := zenserver.New(a, nil, nil, nil, nil, nil, nil)
	s.SetBrowser(m)
	host := httptest.NewServer(s.Handler())
	defer host.Close()
	signature := func(purpose string) string {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		n := make([]byte, 16)
		rand.Read(n)
		nonce := hex.EncodeToString(n)
		sig := ed25519.Sign(priv, auth.BuildSignaturePayload(purpose, a.DaemonID(), "fixture-phone", timestamp, nonce))
		return auth.AuthorizationHeaderPrefix + "v1:fixture-phone:" + a.DaemonID() + ":" + timestamp + ":" + nonce + ":" + hex.EncodeToString(sig)
	}
	manage := func(q browser.Request) browser.Response {
		t.Helper()
		raw, _ := json.Marshal(q)
		req, _ := http.NewRequest("POST", host.URL+"/browser", bytes.NewReader(raw))
		req.Header.Set("Authorization", signature("zen-browser"))
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var out browser.Response
		if resp.StatusCode != 200 {
			var problem any
			json.NewDecoder(resp.Body).Decode(&problem)
			t.Fatalf("browser API %d: %v", resp.StatusCode, problem)
		}
		if e = json.NewDecoder(resp.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	// This site has no personal data or real identity provider.
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" && r.Method == "POST" {
			r.ParseForm()
			if r.Form.Get("name") != "fixture" {
				http.Error(w, "fixture only", 400)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "persistent", Value: "yes", Path: "/", MaxAge: 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, "/account", 303)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/account" {
			_, p := r.Cookie("persistent")
			_, session := r.Cookie("session")
			fmt.Fprintf(w, `<!doctype html><button>Persistent authenticated %t</button><button>Session authenticated %t</button><button onclick="window.open('/popup','_blank')">Open popup</button><button onclick="alert('Fixture dialog')">Show dialog</button><div style='width:120px;height:20px' id='motion'></div><script>localStorage.setItem('fixture','retained');let motion=0;setInterval(()=>document.getElementById('motion').style.backgroundColor=motion++%%2?'blue':'red',120)</script>`, p == nil, session == nil)
			return
		}
		if r.URL.Path == "/popup" {
			fmt.Fprint(w, `<button>Popup fixture ready</button>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><style>body{margin:20px;font:20px sans-serif}input,button{display:block;width:260px;height:48px;margin-bottom:20px}</style><h1>Local Browser sign-in</h1><form action="/login" method="post"><input aria-label="Fixture name" name="name" autofocus><button>Sign in locally</button></form>`)
	}))
	defer site.Close()
	created := manage(browser.Request{Action: "create", Name: "Browser-first fixture"})
	id := created.Resource.ID
	started := manage(browser.Request{Action: "start", ID: id})
	generation := started.Resource.Generation
	manage(browser.Request{Action: "grant", ID: id, AllowAgents: true})
	type viewer struct {
		c        *websocket.Conn
		mu       sync.Mutex
		messages chan map[string]json.RawMessage
		done     chan error
	}
	frames := 0
	var frameSequences []uint64
	var frameMu sync.Mutex
	dial := func() *viewer {
		t.Helper()
		header := http.Header{"Authorization": []string{signature("zen-browser-view:" + id)}}
		c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+"/browser/viewer?id="+id, header)
		if e != nil {
			t.Fatal(e)
		}
		v := &viewer{c: c, messages: make(chan map[string]json.RawMessage, 8), done: make(chan error, 1)}
		go func() {
			for {
				var msg map[string]json.RawMessage
				if e := c.ReadJSON(&msg); e != nil {
					v.done <- e
					return
				}
				if string(msg["type"]) == `"frame"` {
					var data string
					json.Unmarshal(msg["data"], &data)
					jpg, _ := base64.StdEncoding.DecodeString(data)
					config, decodeErr := jpeg.DecodeConfig(bytes.NewReader(jpg))
					var metadata struct{ DeviceWidth, DeviceHeight int }
					json.Unmarshal(msg["metadata"], &metadata)
					if decodeErr != nil || config.Width != 1280 || config.Height != 800 || metadata.DeviceWidth != config.Width || metadata.DeviceHeight != config.Height {
						t.Errorf("frame/input coordinate mismatch: JPEG %dx%d, metadata %dx%d, decode %v", config.Width, config.Height, metadata.DeviceWidth, metadata.DeviceHeight, decodeErr)
					}
					var seq uint64
					json.Unmarshal(msg["seq"], &seq)
					frameMu.Lock()
					frames++
					frameSequences = append(frameSequences, seq)
					frameMu.Unlock()
					if evidence != "" {
						os.WriteFile(filepath.Join(evidence, "product-viewer-latest.jpg"), jpg, 0600)
					}
					v.mu.Lock()
					c.WriteJSON(map[string]any{"type": "ack", "seq": seq})
					v.mu.Unlock()
				} else {
					v.messages <- msg
				}
			}
		}()
		return v
	}
	v := dial()
	defer func() { v.c.Close() }()
	var lease *browser.Lease
	callViewer := func(kind string, c *browser.Command) browser.Response {
		t.Helper()
		v.mu.Lock()
		e := v.c.WriteJSON(map[string]any{"type": kind, "request_id": uuid.NewString(), "lease": lease, "command": c})
		v.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
		select {
		case msg := <-v.messages:
			if raw := msg["error"]; len(raw) > 0 {
				t.Fatalf("viewer error: %s", raw)
			}
			var result browser.Response
			json.Unmarshal(msg["response"], &result)
			if result.Lease != nil {
				lease = result.Lease
			}
			return result
		case e := <-v.done:
			t.Fatalf("viewer closed: %v", e)
		case <-time.After(20 * time.Second):
			t.Fatal("viewer command timed out")
		}
		return browser.Response{}
	}
	callViewer("control", nil)
	callViewer("command", &browser.Command{Kind: "navigate", URL: site.URL})
	callViewer("command", &browser.Command{Kind: "input", Input: &browser.Input{Kind: "text", Text: "fixture"}})
	callViewer("command", &browser.Command{Kind: "press", Text: "Tab"})
	callViewer("command", &browser.Command{Kind: "press", Text: "Enter"})
	// Navigation is asynchronous; bounded polling reads the synthetic page.
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := callViewer("command", &browser.Command{Kind: "snapshot"})
		if strings.Contains(string(r.Result), "Persistent authenticated true") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("prelogin failed: %s", r.Result)
		}
		time.Sleep(100 * time.Millisecond)
	}
	motionDeadline := time.Now().Add(4 * time.Second)
	for {
		frameMu.Lock()
		n := frames
		frameMu.Unlock()
		if n >= 5 {
			break
		}
		if time.Now().After(motionDeadline) {
			frameMu.Lock()
			detail := append([]uint64(nil), frameSequences...)
			frameMu.Unlock()
			t.Fatalf("moving frames stalled: %v", detail)
		}
		time.Sleep(100 * time.Millisecond)
	}
	tab := lease.Target
	if evidence != "" {
		time.Sleep(250 * time.Millisecond)
		data, _ := os.ReadFile(filepath.Join(evidence, "product-viewer-latest.jpg"))
		os.WriteFile(filepath.Join(evidence, "product-after-prelogin.jpg"), data, 0600)
	}
	if os.Getenv("ZEN_BROWSER_PROVIDER") != "" {
		callViewer("release", nil)
		browserNormalProviderProof(t, m, id, tab, root, a, signature)
		callViewer("control", nil)
	}
	// A real MCP client launches the actual stdio tool command, attached through
	// the real local control socket. Only task liveness is a test watcher.
	watcher := newFakeControlWatcher()
	watcher.workers["fixture-task"] = &classifier.Worker{ID: "fixture-task", BrowserID: id, State: classifier.StateRunning}
	app := &controlApp{browsers: m, watcher: watcher}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cs := &control.Server{Path: filepath.Join(root, "run", "zen.sock"), Handler: app}
	controlDone := make(chan error, 1)
	go func() { controlDone <- cs.Run(ctx) }()
	defer func() { cancel(); <-controlDone }()
	for i := 0; i < 100; i++ {
		if _, e := os.Stat(cs.Path); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	attach := func() *mcp.ClientSession {
		t.Helper()
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestBrowserMCPHelper$")
		cmd.Env = append(os.Environ(), "ZEN_BROWSER_MCP_FIXTURE=1", "ZEN_BROWSER_FIXTURE_ID="+id, "ZEN_BROWSER_FIXTURE_STATE="+root)
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-agent", Version: "1"}, nil)
		session, e := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	session := attach()
	tool := func(action string) (string, bool) {
		t.Helper()
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "browser", Arguments: map[string]any{"action": action}})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(r)
		return string(raw), r.IsError
	}
	if _, denied := tool("control"); !denied {
		t.Fatal("Agent acquired human control")
	}
	callViewer("release", nil)
	if detail, denied := tool("control"); denied {
		t.Fatal("later Agent attachment failed", detail)
	}
	observed, denied := tool("snapshot")
	if denied || !strings.Contains(observed, "Persistent authenticated true") {
		t.Fatal(observed)
	}
	if !strings.Contains(observed, tab) {
		t.Fatal("Agent attached different tab")
	}
	tool("release")
	session.Close()
	session = attach()
	defer session.Close()
	tool("control")
	observed, denied = tool("snapshot")
	if denied || !strings.Contains(observed, "Session authenticated true") {
		t.Fatal("reattach lost live session", observed)
	}
	callViewer("control", nil)
	if _, denied = tool("snapshot"); !denied {
		t.Fatal("Agent read during human control")
	}
	// Actual window.open popup, exact target selection and JavaScript dialog.
	current := callViewer("command", &browser.Command{Kind: "snapshot"})
	var view struct {
		Refs map[string]struct{ Name string }
	}
	json.Unmarshal(current.Result, &view)
	findRef := func(name string) string {
		for ref, item := range view.Refs {
			if item.Name == name {
				return "@" + ref
			}
		}
		t.Fatalf("missing %s", name)
		return ""
	}
	callViewer("command", &browser.Command{Kind: "click", Ref: findRef("Open popup")})
	listed := callViewer("command", &browser.Command{Kind: "tabs"})
	var popupTabs []struct{ ID, URL string }
	json.Unmarshal(listed.Result, &popupTabs)
	var opened string
	for _, item := range popupTabs {
		if strings.HasSuffix(item.URL, "/popup") {
			opened = item.ID
		}
	}
	if opened == "" {
		t.Fatal("window.open popup not listed")
	}
	callViewer("command", &browser.Command{Kind: "select_tab", Target: opened})
	callViewer("command", &browser.Command{Kind: "select_tab", Target: tab})
	current = callViewer("command", &browser.Command{Kind: "snapshot"})
	json.Unmarshal(current.Result, &view)
	callViewer("command", &browser.Command{Kind: "click", Ref: findRef("Show dialog")})
	dialog := callViewer("command", &browser.Command{Kind: "dialog_status"})
	if !strings.Contains(string(dialog.Result), "Fixture dialog") {
		t.Fatalf("dialog not surfaced: %s", dialog.Result)
	}
	callViewer("command", &browser.Command{Kind: "dialog_accept"})
	// Explicit new tab is a separate supported action.

	callViewer("command", &browser.Command{Kind: "new_tab", URL: site.URL + "/popup"})
	popupTarget := lease.Target
	popup := callViewer("command", &browser.Command{Kind: "snapshot"})
	if !strings.Contains(string(popup.Result), "Popup fixture ready") || popupTarget == tab {
		t.Fatalf("popup target: %s", popup.Result)
	}
	callViewer("command", &browser.Command{Kind: "select_tab", Target: tab})
	callViewer("release", nil)
	v.mu.Lock()
	v.c.WriteJSON(map[string]any{"type": "close"})
	v.mu.Unlock()
	if e := <-v.done; !websocket.IsCloseError(e, 1000) {
		t.Fatalf("viewer close: %v", e)
	}
	v.c.Close()
	time.Sleep(100 * time.Millisecond)
	v = dial()
	lease = nil
	callViewer("control", nil)
	same := callViewer("command", &browser.Command{Kind: "snapshot"})
	if !strings.Contains(string(same.Result), "Persistent authenticated true") {
		t.Fatal("reconnect lost login")
	}
	v.mu.Lock()
	v.c.WriteJSON(map[string]any{"type": "close"})
	v.mu.Unlock()
	<-v.done
	v.c.Close()
	manage(browser.Request{Action: "stop", ID: id})
	if os.Getenv("ZEN_BROWSER_SECURE") == "1" {
		// Inspect only this disposable synthetic cookie database. v11 proves use
		// of a platform key; Linux basic fallback writes v10 instead.
		check := exec.Command("python3", "-c", "import sqlite3,sys; r=sqlite3.connect(sys.argv[1]).execute(\"select length(value),hex(substr(encrypted_value,1,3)) from cookies where name='persistent'\").fetchone(); assert r==(0,'763131'),r", filepath.Join(root, "browsers", id, "profile", "Default", "Cookies"))
		if out, err := check.CombinedOutput(); err != nil {
			t.Fatalf("secure cookie storage: %v: %s", err, out)
		}
	}
	restarted := manage(browser.Request{Action: "start", ID: id})
	if restarted.Resource.Generation == generation {
		t.Fatal("restart kept generation")
	}
	v = dial()
	lease = nil
	callViewer("control", nil)
	callViewer("command", &browser.Command{Kind: "navigate", URL: site.URL + "/account"})
	after := callViewer("command", &browser.Command{Kind: "snapshot"})
	if !strings.Contains(string(after.Result), "Persistent authenticated true") || !strings.Contains(string(after.Result), "Session authenticated false") {
		t.Fatalf("restart semantics: %s", after.Result)
	}
	v.mu.Lock()
	v.c.WriteJSON(map[string]any{"type": "close"})
	v.mu.Unlock()
	<-v.done
	v.c.Close()
	frameMu.Lock()
	frameCount := frames
	frameMu.Unlock()
	report := map[string]any{"browser_first": true, "product_authenticated_viewer": true, "later_mcp_attach_same_tab": true, "detach_reattach": true, "human_blocks_agent": true, "viewer_reconnect": true, "close_code": 1000, "new_tab_select": true, "window_open_popup": true, "javascript_dialog": true, "persistent_cookie_restart": true, "session_cookie_lost": true, "frames": frameCount, "frame_sequences": frameSequences, "store": "basic fixture only; secure Secret Service restart not tested", "actual_provider_task": false}
	if os.Getenv("ZEN_BROWSER_SECURE") == "1" {
		report["store"] = "production libsecret with isolated installed KWallet Secret Service; v11 encrypted cookie verified"
		report["secure_store_restart"] = true
	}
	if os.Getenv("ZEN_BROWSER_PROVIDER") == "1" {
		report["actual_provider_task"] = true
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	if evidence != "" {
		os.WriteFile(filepath.Join(evidence, "product-browser-first-result.json"), raw, 0600)
	}
	t.Log(string(raw))
}
