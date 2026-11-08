//go:build linux

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

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	mewlaserver "github.com/daoleno/mewla/daemon/server"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBrowserMCPHelper(t *testing.T) {
	if os.Getenv("MEWLA_BROWSER_MCP_FIXTURE") != "1" {
		return
	}
	err := runBrowserCommand([]string{"mcp", "--task", os.Getenv("MEWLA_BROWSER_FIXTURE_TASK"), "--state-dir", os.Getenv("MEWLA_BROWSER_FIXTURE_STATE")}, os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

// TestAgentWindowsProductFlow runs the real product path (the production
// backend, no fixture) on an isolated graphical stack: two Workers each ask
// for control through the real MCP tool and control socket, get a window of
// their own on the one browser, work in them at the same time, are watched
// through the authenticated viewer, and a sign-in the person makes in one
// window survives the browser being stopped and started again.
//
//	MEWLA_BROWSER_INTEGRATION=1 go test ./cmd/mewla -run TestAgentWindowsProductFlow -count=1 -v
func TestAgentWindowsProductFlow(t *testing.T) {
	if os.Getenv("MEWLA_BROWSER_INTEGRATION") != "1" {
		t.Skip("opt-in isolated graphical browser")
	}
	evidence := os.Getenv("MEWLA_BROWSER_EVIDENCE")
	root := t.TempDir()
	m, err := browser.New(filepath.Join(root, "browsers"), browser.NewBackend())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	owner := browser.Owner{Kind: "human", ID: "server-owner"}
	on := true
	if _, err := m.Handle(context.Background(), owner, browser.Request{Action: "settings", Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	a, err := auth.NewManager(filepath.Join(root, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pair, _ := a.IssuePairingToken(time.Minute)
	if _, err = a.EnrollDevice(pair.Value, a.DaemonID(), a.PublicKeyHex(), "fixture-phone", "Fixture", hex.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	s := mewlaserver.New(a, nil, nil, nil, nil, nil, nil)
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
	list := func() []browser.Resource {
		t.Helper()
		raw, _ := json.Marshal(browser.Request{Action: "list"})
		req, _ := http.NewRequest("POST", host.URL+"/browser", bytes.NewReader(raw))
		req.Header.Set("Authorization", signature("mewla-browser"))
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var out browser.Response
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil {
			t.Fatalf("list: %d", resp.StatusCode)
		}
		return out.Resources
	}
	// This site has no personal data or real identity provider.
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch {
		case r.URL.Path == "/login" && r.Method == "POST":
			r.ParseForm()
			http.SetCookie(w, &http.Cookie{Name: "persistent", Value: "yes", Path: "/", MaxAge: 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, "/account", 303)
		case r.URL.Path == "/account":
			_, p := r.Cookie("persistent")
			_, session := r.Cookie("session")
			fmt.Fprintf(w, `<!doctype html><button>Persistent authenticated %t</button><button>Session authenticated %t</button><div style='width:120px;height:20px' id='motion'></div><script>let motion=0;setInterval(()=>document.getElementById('motion').style.backgroundColor=motion++%%2?'blue':'red',120)</script>`, p == nil, session == nil)
		case strings.HasPrefix(r.URL.Path, "/page/"):
			fmt.Fprintf(w, `<!doctype html><h1>Page %s</h1><button>Button %s</button><div style='width:120px;height:20px' id='motion'></div><script>let motion=0;setInterval(()=>document.getElementById('motion').style.backgroundColor=motion++%%2?'blue':'red',120)</script>`, r.URL.Path[6:], r.URL.Path[6:])
		default:
			fmt.Fprint(w, `<!doctype html><style>body{margin:20px;font:20px sans-serif}input,button{display:block;width:260px;height:48px;margin-bottom:20px}</style><h1>Local Browser sign-in</h1><form action="/login" method="post"><input aria-label="Fixture name" name="name" autofocus><button>Sign in locally</button></form>`)
		}
	}))
	defer site.Close()

	// A real MCP client launches the actual stdio tool command, attached through
	// the real local control socket. Only task liveness is a test watcher.
	watcher := newFakeControlWatcher()
	for _, id := range []string{"worker-a", "worker-b"} {
		watcher.workers[id] = &classifier.Worker{ID: id, State: classifier.StateRunning, Delegated: true}
	}
	app := &controlApp{browsers: m, watcher: watcher}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	attach := func(task string) *mcp.ClientSession {
		t.Helper()
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestBrowserMCPHelper$")
		cmd.Env = append(os.Environ(), "MEWLA_BROWSER_MCP_FIXTURE=1", "MEWLA_BROWSER_FIXTURE_TASK="+task, "MEWLA_BROWSER_FIXTURE_STATE="+root)
		client := mcp.NewClient(&mcp.Implementation{Name: "fixture-agent-" + task, Version: "1"}, nil)
		session, e := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	tool := func(session *mcp.ClientSession, args map[string]any) (string, bool) {
		t.Helper()
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "browser", Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(r)
		return string(raw), r.IsError
	}
	sessions := map[string]*mcp.ClientSession{"worker-a": attach("worker-a"), "worker-b": attach("worker-b")}
	defer func() {
		for _, c := range sessions {
			c.Close()
		}
	}()

	// Both ask at once: one browser starts, each gets its own window.
	var wg sync.WaitGroup
	for task, session := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if detail, denied := tool(session, map[string]any{"action": "control"}); denied {
				t.Errorf("%s: control failed: %s", task, detail)
			}
		}()
	}
	wg.Wait()
	rows := list()
	if len(rows) != 2 || rows[0].ID == rows[1].ID || rows[0].Target == rows[1].Target || rows[0].State != "running" {
		t.Fatalf("Screen rows: %+v", rows)
	}

	// Each works on a page of its own, together, and sees only its own.
	for round := 0; round < 3; round++ {
		for task, session := range sessions {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mark := strings.TrimPrefix(task, "worker-")
				if detail, denied := tool(session, map[string]any{"action": "navigate", "url": site.URL + "/page/" + mark}); denied {
					t.Errorf("%s: navigate: %s", task, detail)
					return
				}
				seen, denied := tool(session, map[string]any{"action": "snapshot"})
				other := map[string]string{"a": "b", "b": "a"}[mark]
				if denied || !strings.Contains(seen, "Button "+mark) || strings.Contains(seen, "Button "+other) {
					t.Errorf("%s saw %s", task, seen)
				}
			}()
		}
		wg.Wait()
	}

	// An agent can read the page's text, not only its controls.
	if text, denied := tool(sessions["worker-a"], map[string]any{"action": "read"}); denied || !strings.Contains(text, "Page a") {
		t.Fatalf("read: %s", text)
	}

	// The person watches each window over the authenticated viewer.
	type viewer struct {
		c        *websocket.Conn
		mu       sync.Mutex
		messages chan map[string]json.RawMessage
		frames   chan []byte
	}
	dial := func(id string) *viewer {
		t.Helper()
		header := http.Header{"Authorization": []string{signature("mewla-browser-view:" + id)}}
		c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+"/browser/viewer?id="+id, header)
		if e != nil {
			t.Fatal(e)
		}
		v := &viewer{c: c, messages: make(chan map[string]json.RawMessage, 8), frames: make(chan []byte, 64)}
		go func() {
			for {
				var msg map[string]json.RawMessage
				if e := c.ReadJSON(&msg); e != nil {
					return
				}
				if string(msg["type"]) != `"frame"` {
					v.messages <- msg
					continue
				}
				var data string
				json.Unmarshal(msg["data"], &data)
				jpg, _ := base64.StdEncoding.DecodeString(data)
				if config, e := jpeg.DecodeConfig(bytes.NewReader(jpg)); e != nil || config.Width != 1280 || config.Height != 800 {
					t.Errorf("frame is %dx%d (%v), want 1280x800", config.Width, config.Height, e)
				}
				select {
				case v.frames <- jpg:
				default:
				}
				var seq uint64
				json.Unmarshal(msg["seq"], &seq)
				v.mu.Lock()
				c.WriteJSON(map[string]any{"type": "ack", "seq": seq})
				v.mu.Unlock()
			}
		}()
		return v
	}
	for i, row := range rows {
		v := dial(row.ID)
		got := 0
		for deadline := time.After(8 * time.Second); got < 4; {
			select {
			case jpg := <-v.frames:
				got++
				if evidence != "" {
					os.WriteFile(filepath.Join(evidence, fmt.Sprintf("window-%d.jpg", i)), jpg, 0600)
				}
			case <-deadline:
				t.Fatalf("window %d streamed %d frames", i, got)
			}
		}
		v.c.Close()
	}

	// The person takes one window to sign in, once.
	person := rows[0]
	v := dial(person.ID)
	var lease *browser.Lease
	viewerCall := func(kind string, c *browser.Command) browser.Response {
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
		case <-time.After(20 * time.Second):
			t.Fatal("viewer command timed out")
		}
		return browser.Response{}
	}
	viewerCall("control", nil)
	viewerCall("command", &browser.Command{Kind: "navigate", URL: site.URL})
	viewerCall("command", &browser.Command{Kind: "input", Input: &browser.Input{Kind: "text", Text: "fixture"}})
	viewerCall("command", &browser.Command{Kind: "press", Text: "Tab"})
	viewerCall("command", &browser.Command{Kind: "press", Text: "Enter"})
	for deadline := time.Now().Add(6 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		r := viewerCall("command", &browser.Command{Kind: "snapshot"})
		if strings.Contains(string(r.Result), "Persistent authenticated true") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sign-in failed: %s", r.Result)
		}
	}
	viewerCall("release", nil)
	v.c.Close()

	// Both Workers end: their windows close and the browser stops; the next
	// Worker's request starts it again on the same profile, still signed in.
	for task, session := range sessions {
		tool(session, map[string]any{"action": "release"})
		m.Forget(task)
	}
	for deadline := time.Now().Add(15 * time.Second); len(list()) != 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("windows stayed after their Workers ended")
		}
	}
	time.Sleep(6 * time.Second) // the browser stops with its last window
	late := attach("worker-b")
	defer late.Close()
	if detail, denied := tool(late, map[string]any{"action": "control"}); denied {
		t.Fatal("control after the browser stopped:", detail)
	}
	tool(late, map[string]any{"action": "navigate", "url": site.URL + "/account"})
	after, _ := tool(late, map[string]any{"action": "snapshot"})
	if !strings.Contains(after, "Persistent authenticated true") || !strings.Contains(after, "Session authenticated false") {
		t.Fatalf("restart semantics: %s", after)
	}
	t.Log("two agents, one browser, one profile: sign-in kept across a browser restart")
}
