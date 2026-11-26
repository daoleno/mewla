// mewla-model-gate walks the model picker matrix against real agents on an
// isolated daemon and prints one row per agent × moment: whether the Session
// offers a model choice, what the daemon answered to a pick, and what the
// agent's own record says its next turn ran on (Claude's transcript, Codex's
// rollout, OpenCode's messages, Pi's session file). Mewla's own label is
// never the evidence.
//
// It is the release gate for "pick each agent's model" (docs/ci-release.md);
// scripts/model-picker-gate.sh starts the isolated daemon and runs it:
//
//	mewla-model-gate pair -link <pairing link> -server http://127.0.0.1:PORT -key key.json
//	mewla-model-gate run -server ... -key key.json -cwd DIR [-agents claude,codex,...] [-restart CMD]
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mewla-model-gate pair|run [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "pair":
		err = pair(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mewla-model-gate:", err)
		os.Exit(1)
	}
}

// ---- pairing and the WebSocket, as the app does them ----

type identity struct {
	DeviceID   string `json:"device_id"`
	DaemonID   string `json:"daemon_id"`
	PrivateKey string `json:"private_key"`
}

func pair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	link := fs.String("link", "", "pairing link (mewla pair -details prints it)")
	server := fs.String("server", "", "http://host:port")
	keyPath := fs.String("key", "", "identity file to write")
	_ = fs.Parse(args)
	raw := *link
	if at := strings.Index(raw, "#pair="); at >= 0 {
		raw, _ = url.QueryUnescape(raw[at+len("#pair="):])
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	compact := parsed.Query().Get("p")
	if compact == "" {
		return errors.New("the link has no p= pairing payload")
	}
	payload, err := base64.RawURLEncoding.DecodeString(compact)
	if err != nil || len(payload) < 3 {
		return fmt.Errorf("pairing payload: %v", err)
	}
	urlLen := int(binary.BigEndian.Uint16(payload[1:3]))
	rest := payload[3+urlLen:]
	if len(rest) < 33 {
		return errors.New("pairing payload too short")
	}
	daemonKey, token := hex.EncodeToString(rest[:32]), hex.EncodeToString(rest[32:])
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	deviceID := "model-gate-" + hex.EncodeToString(public[:6])
	body, _ := json.Marshal(map[string]any{
		"enrollment_token": token, "expected_daemon_public_key": daemonKey,
		"device_id": deviceID, "device_name": "Model gate", "device_public_key": hex.EncodeToString(public),
	})
	response, err := http.Post(strings.TrimRight(*server, "/")+"/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var reply map[string]any
	_ = json.NewDecoder(response.Body).Decode(&reply)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("pair: HTTP %d", response.StatusCode)
	}
	daemonID, _ := reply["daemon_id"].(string)
	out, _ := json.Marshal(identity{DeviceID: deviceID, DaemonID: daemonID, PrivateKey: hex.EncodeToString(private)})
	return os.WriteFile(*keyPath, out, 0o600)
}

type client struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	waiters map[string]chan map[string]any
	done    chan struct{}
}

func dial(server, keyPath string) (*client, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	var id identity
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, err
	}
	keyBytes, err := hex.DecodeString(id.PrivateKey)
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(ed25519.PrivateKey(keyBytes), auth.BuildSignaturePayload("mewla-connect", id.DaemonID, id.DeviceID, timestamp, nonceHex))
	header := http.Header{}
	header.Set("Authorization", auth.AuthorizationHeaderPrefix+"v1:"+id.DeviceID+":"+id.DaemonID+":"+timestamp+":"+nonceHex+":"+hex.EncodeToString(signature))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(strings.TrimRight(server, "/"), "http")+"/ws", header)
	if err != nil {
		return nil, err
	}
	c := &client{conn: conn, waiters: map[string]chan map[string]any{}, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *client) read() {
	defer close(c.done)
	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		id, _ := message["request_id"].(string)
		if id == "" {
			// Some answers (the Session list) carry no request id.
			kind, _ := message["type"].(string)
			id = "type:" + kind
		}
		c.mu.Lock()
		ch := c.waiters[id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- message:
			default:
			}
		}
	}
}

var requestSeq struct {
	sync.Mutex
	n int
}

// request sends a message and waits for the reply of one of the types (or an error).
func (c *client) request(message map[string]any, timeout time.Duration, types ...string) (map[string]any, error) {
	requestSeq.Lock()
	requestSeq.n++
	id := fmt.Sprintf("gate-%d-%d", time.Now().UnixNano(), requestSeq.n)
	requestSeq.Unlock()
	message["request_id"] = id
	ch := make(chan map[string]any, 8)
	c.mu.Lock()
	c.waiters[id] = ch
	for _, kind := range types {
		c.waiters["type:"+kind] = ch
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, id)
		for _, kind := range types {
			if c.waiters["type:"+kind] == ch {
				delete(c.waiters, "type:"+kind)
			}
		}
		c.mu.Unlock()
	}()
	raw, _ := json.Marshal(message)
	c.mu.Lock()
	err := c.conn.WriteMessage(websocket.TextMessage, raw)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	deadline := time.After(timeout)
	for {
		select {
		case reply := <-ch:
			kind, _ := reply["type"].(string)
			if kind == "error" || kind == "input_failed" {
				msg, _ := reply["message"].(string)
				return reply, errors.New(msg)
			}
			for _, want := range types {
				if kind == want {
					return reply, nil
				}
			}
		case <-deadline:
			return nil, fmt.Errorf("no %s within %s", strings.Join(types, "/"), timeout)
		case <-c.done:
			return nil, errors.New("connection closed")
		}
	}
}

// ---- the matrix ----

type choice struct{ model, effort string }

func (c choice) String() string {
	if c.effort == "" {
		return c.model
	}
	return c.model + "/" + c.effort
}

// defaultTargets are cheap picks per agent: the first for the switch before
// the first message and while working, the second for the switch after it.
var defaultTargets = map[string][2]choice{
	"claude":      {{"haiku", "low"}, {"sonnet", "low"}},
	"codex":       {{"gpt-6-luna", "low"}, {"gpt-6-luna", "medium"}},
	"opencode":    {{"opencode/tidepool-alpha-free", "low"}, {"opencode-go/deepseek-v4.1-flash", "low"}},
	"pi":          {{"opencode-go/deepseek-v4.1-flash", "low"}, {"opencode-go/deepseek-v4.1-flash", "high"}},
	"antigravity": {{"gemini-4-carbon", "low"}, {"gemini-4-neon", "low"}},
	"dsh":         {{"deepseek-v4.1-flash", "low"}, {"deepseek-v4.1-flash", "high"}},
}

var defaultCommands = map[string]string{
	"claude": "claude --model haiku", "codex": "codex", "opencode": "opencode", "pi": "pi",
	"cursor": "cursor-agent", "grok": "grok", "antigravity": "agy", "dsh": "dsh",
}

type cell struct {
	agent, moment string
	offered       bool
	reply         string
	want          choice
	evidence      string
	pass          bool
	note          string
}

type session struct {
	agent, id, command, cwd string
	created                 time.Time
	// connection is the runtime's own connection id: the agent's, or the
	// Provider connection of a Session on a route.
	connection string
}

type gate struct {
	c        *client
	server   string
	key      string
	cwd      string
	restart  string
	turnWait time.Duration
	cells    []cell
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	server := fs.String("server", "", "http://host:port of the isolated daemon")
	key := fs.String("key", "", "identity file from pair")
	cwd := fs.String("cwd", "", "a scratch folder the Sessions start in")
	agents := fs.String("agents", "claude,codex,opencode,pi,cursor,grok,antigravity,dsh", "agents to walk")
	restart := fs.String("restart", "", "shell command that restarts the isolated daemon (adds the after-restart moment)")
	out := fs.String("out", "", "also write the table here")
	ask := fs.Bool("ask", false, "start Claude asking before it runs a command, so its busy moment is a wait for an answer")
	_ = fs.Parse(args)
	if *ask {
		defaultCommands["claude"] = "claude --model haiku --permission-mode default"
	}
	if *server == "" || *key == "" || *cwd == "" {
		return errors.New("-server, -key and -cwd are required")
	}
	c, err := dial(*server, *key)
	if err != nil {
		return err
	}
	g := &gate{c: c, server: *server, key: *key, cwd: *cwd, restart: *restart, turnWait: 4 * time.Minute}
	var sessions []*session
	for _, agent := range strings.Split(*agents, ",") {
		agent = strings.TrimSpace(agent)
		if agent == "" {
			continue
		}
		s, err := g.create(agent)
		if err != nil {
			g.add(cell{agent: agent, moment: "start", note: err.Error()})
			continue
		}
		sessions = append(sessions, s)
		g.walk(s)
	}
	if g.restart != "" && len(sessions) > 0 {
		g.afterRestart(sessions)
	}
	table := g.table()
	fmt.Print(table)
	if *out != "" {
		_ = os.WriteFile(*out, []byte(table), 0o644)
	}
	for _, cl := range g.cells {
		if !cl.pass {
			return errors.New("the matrix has failing cells")
		}
	}
	return nil
}

func (g *gate) add(cl cell) {
	g.cells = append(g.cells, cl)
	mark := "FAIL"
	if cl.pass {
		mark = "pass"
	}
	fmt.Fprintf(os.Stderr, "%s %-11s %-13s %s | %s | %s\n", mark, cl.agent, cl.moment, cl.reply, cl.evidence, cl.note)
}

func (g *gate) create(agent string) (*session, error) {
	command := defaultCommands[agent]
	if command == "" {
		command = agent
	}
	reply, err := g.c.request(map[string]any{"type": "create_session", "cwd": g.cwd, "command": command, "name": "model-gate-" + agent}, 20*time.Second, "session_created")
	if err != nil {
		return nil, err
	}
	s := &session{agent: agent, cwd: g.cwd, created: time.Now()}
	s.id, _ = reply["worker_id"].(string)
	if ws, ok := reply["worker_session"].(map[string]any); ok {
		s.command, _ = ws["command"].(string)
	}
	return s, nil
}

// offered reads whether the Session offers a model choice now, waiting up to
// wait for the agent to link up.
func (g *gate) offered(s *session, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		reply, err := g.c.request(map[string]any{"type": "list_worker_sessions"}, 10*time.Second, "worker_session_list")
		if err == nil {
			list, _ := reply["worker_sessions"].([]any)
			for _, raw := range list {
				w, _ := raw.(map[string]any)
				if w["id"] != s.id {
					continue
				}
				if command, _ := w["command"].(string); command != "" && s.command == "" {
					s.command = command
				}
				caps, _ := w["capabilities"].(map[string]any)
				if caps["model_profile_active_switch"] == true {
					return true
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Second)
	}
}

// approvals are the decisions the Session waits on now.
func (g *gate) approvals(s *session) []map[string]any {
	reply, err := g.c.request(map[string]any{"type": "list_worker_sessions"}, 10*time.Second, "worker_session_list")
	if err != nil {
		return nil
	}
	list, _ := reply["worker_sessions"].([]any)
	for _, raw := range list {
		w, _ := raw.(map[string]any)
		if w["id"] != s.id {
			continue
		}
		caps, _ := w["capabilities"].(map[string]any)
		pending, _ := caps["approvals"].([]any)
		out := []map[string]any{}
		for _, p := range pending {
			if m, ok := p.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// approveUntil allows, once, every command the Session asks about until stop.
func (g *gate) approveUntil(s *session, stop chan struct{}) {
	for {
		for _, asked := range g.approvals(s) {
			options, _ := asked["options"].([]any)
			chosen := ""
			for _, raw := range options {
				o, _ := raw.(map[string]any)
				id, _ := o["id"].(string)
				label, _ := o["label"].(string)
				scope, _ := o["scope"].(string)
				if chosen == "" && (scope == "once" || strings.HasPrefix(strings.ToLower(label), "yes") || strings.HasPrefix(strings.ToLower(label), "allow")) {
					chosen = id
				}
			}
			if id, _ := asked["id"].(string); id != "" && chosen != "" {
				_, _ = g.c.request(map[string]any{"type": "approval_decision", "worker_id": s.id, "id": id, "option_id": chosen}, 10*time.Second, "approval_decided")
			}
		}
		// Claude's own question in the terminal ("Do you want to proceed?"):
		// its first option is Yes.
		if claudeAsks(g.screenText(s)) {
			_, _ = g.c.request(map[string]any{"type": "send_key", "worker_id": s.id, "key": "Enter"}, 3*time.Second, "send_key_result")
		}
		select {
		case <-stop:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// claudeAsks reads Claude's permission question on its screen.
func claudeAsks(screen string) bool {
	return strings.Contains(screen, "Do you want to proceed?")
}

type runtimeView struct {
	connection       string
	current, pending choice
	models           map[string][]string
	names            map[string]string
	order            []string
	// failed is a held pick the agent did not take, and its reason.
	failed, failedReason string
}

// switchLands bounds how long a pick may take to be in force: the answer to
// an idle pick, and a held pick after the turn it waited for ended. The
// Model sheet's hourglass is never longer (11-25: it looked endless).
const switchLands = 15 * time.Second

func (g *gate) runtime(s *session) (runtimeView, error) {
	reply, err := g.c.request(map[string]any{"type": "get_thread_runtime", "worker_id": s.id}, 40*time.Second, "thread_runtime")
	if err != nil {
		return runtimeView{}, err
	}
	view := parseRuntime(reply)
	if view.connection != "" {
		s.connection = view.connection
	}
	return view, nil
}

func parseRuntime(reply map[string]any) runtimeView {
	r, _ := reply["runtime"].(map[string]any)
	str := func(key string) string { v, _ := r[key].(string); return v }
	view := runtimeView{connection: str("connection_id"), current: choice{str("model_id"), str("reasoning_effort")}, pending: choice{str("pending_model_id"), str("pending_reasoning_effort")}, models: map[string][]string{}, names: map[string]string{},
		failed: str("failed_model_id"), failedReason: str("failed_reason")}
	models, _ := r["native_models"].([]any)
	for _, raw := range models {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		var efforts []string
		list, _ := m["reasoning_efforts"].([]any)
		for _, e := range list {
			if s, ok := e.(string); ok {
				efforts = append(efforts, s)
			}
		}
		view.models[id] = efforts
		view.names[id], _ = m["display_name"].(string)
		view.order = append(view.order, id)
	}
	return view
}

// pick sends set_thread_runtime the way the Model sheet does and says what
// came back: "applied", "pending" or the refusal.
func (g *gate) pick(s *session, want choice) (string, bool) {
	connection := s.connection
	if connection == "" {
		connection = providerOf(s.agent)
	}
	runtime := map[string]any{"connection_id": connection, "model_id": want.model}
	if want.effort != "" {
		runtime["effect"] = want.effort
	}
	started := time.Now()
	reply, err := g.c.request(map[string]any{"type": "set_thread_runtime", "worker_id": s.id, "runtime": runtime}, 60*time.Second, "thread_runtime_set")
	if took := time.Since(started); took > switchLands {
		return fmt.Sprintf("answered after %s (more than %s)", took.Round(time.Second), switchLands), false
	}
	if err != nil {
		command := s.command
		if len(command) > 60 {
			command = command[:60] + "…"
		}
		return "refused: " + err.Error() + " (Session command: " + command + ")", false
	}
	view := parseRuntime(reply)
	took := time.Since(started).Round(100 * time.Millisecond)
	switch {
	case view.pending.model == want.model && (want.effort == "" || view.pending.effort == want.effort):
		return fmt.Sprintf("pending (answered in %s)", took), true
	case view.current.model == want.model && (want.effort == "" || view.current.effort == want.effort):
		return fmt.Sprintf("applied in %s", took), true
	}
	// What the Model sheet calls "an invalid activation selection".
	return fmt.Sprintf("echo %s, asked %s", view.current, want), false
}

func providerOf(agent string) string { return agent }

// lands waits for a held pick to be what the Session runs on, with nothing
// pending, within switchLands.
func (g *gate) lands(s *session, want choice) (bool, string) {
	deadline := time.Now().Add(switchLands)
	last := "no read"
	for {
		view, err := g.runtime(s)
		switch {
		case err != nil:
			last = err.Error()
		case view.failed != "":
			return false, "the held pick failed: " + view.failedReason
		case view.pending.model == "" && view.current.matches(s.agent, want):
			return true, ""
		default:
			last = fmt.Sprintf("current %s, pending %s", view.current, view.pending)
		}
		if time.Now().After(deadline) {
			return false, fmt.Sprintf("not in force %s after the turn ended: %s", switchLands, last)
		}
		time.Sleep(time.Second)
	}
}

// claudeLabels checks the menu's names against Claude's own model list, read
// here the way the daemon reads it: each model reads with the version Claude
// names it by ("Opus 5.5"), Default with the model it runs on (11-25: the
// sheet said "Opus", "Default (recommended)").
func (g *gate) claudeLabels(s *session, view runtimeView) {
	cl := cell{agent: s.agent, moment: "labels", offered: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := s.command
	if work.LaunchProgramProvider(command) != work.WorkerProviderClaude {
		command = defaultCommands["claude"]
	}
	own, err := work.ReadClaudeModels(ctx, command, s.cwd, os.Environ())
	if err != nil {
		cl.note = "Claude's own list: " + err.Error()
		g.add(cl)
		return
	}
	named := map[string]string{}
	for _, model := range own {
		if model.Name != "" {
			named[model.Resolved] = model.Name
		}
	}
	var shown, wrong []string
	for _, model := range own {
		label := view.names[model.Value]
		shown = append(shown, label)
		want := model.Name
		if model.Value == "default" {
			want = named[model.Resolved]
		}
		if want != "" && !strings.Contains(label, want) {
			wrong = append(wrong, fmt.Sprintf("%s reads %q, Claude names it %q", model.Value, label, want))
		}
	}
	cl.reply, cl.evidence = strings.Join(shown, ", "), fmt.Sprintf("%d models from Claude", len(own))
	cl.pass = len(own) > 0 && len(wrong) == 0
	cl.note = strings.Join(wrong, "; ")
	g.add(cl)
}

// targets picks two cheap choices the Session's menu offers.
func (g *gate) targets(s *session, view runtimeView) [2]choice {
	t, ok := defaultTargets[s.agent]
	if !ok {
		// Agents without efforts (Cursor, Grok): two models of the menu.
		var picks []choice
		for _, id := range view.order {
			if id != view.current.model {
				picks = append(picks, choice{model: id})
			}
		}
		picks = append(picks, choice{model: view.current.model})
		if len(picks) < 2 {
			picks = append(picks, picks[0])
		}
		return [2]choice{picks[0], picks[1]}
	}
	return t
}

// screenText is the Session's whole screen.
func (g *gate) screenText(s *session) string {
	reply, err := g.c.request(map[string]any{"type": "terminal_snapshot", "target_id": s.id}, 10*time.Second, "terminal_snapshot")
	if err != nil {
		return ""
	}
	text, _ := reply["text"].(string)
	return text
}

// screen is the Session's last screen lines, for a note.
func (g *gate) screen(s *session) string {
	reply, err := g.c.request(map[string]any{"type": "terminal_snapshot", "target_id": s.id}, 10*time.Second, "terminal_snapshot")
	if err != nil {
		return ""
	}
	text, _ := reply["text"].(string)
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " / ")
}

func (g *gate) walk(s *session) {
	offered := g.offered(s, 25*time.Second)
	if text := g.screenText(s); !offered && (strings.Contains(text, "Update available") || strings.Contains(text, "Skip until next version")) {
		// The agent asks about its own update before it starts its
		// conversation; the gate skips it, as a person would answer it.
		_, _ = g.c.request(map[string]any{"type": "send_key", "worker_id": s.id, "key": "Escape"}, 3*time.Second, "send_key_result")
	}
	if !offered && !g.offered(s, 65*time.Second) {
		_, err := g.runtime(s)
		reason := "no menu"
		if err != nil {
			reason = err.Error()
		}
		if screen := g.screen(s); screen != "" {
			reason += " · screen: " + screen
		}
		// Not offered is right only when the agent truly cannot give one now:
		// not signed in, or (OpenCode) before its first message.
		pass := err != nil || s.agent == "opencode"
		g.add(cell{agent: s.agent, moment: "new", reply: "not offered", pass: pass, note: reason})
		if s.agent != "opencode" {
			return
		}
		// OpenCode's terminal UI holds the model until its first message;
		// the choice is offered once the session exists.
		if _, err := g.turn(s, "Reply with exactly the word: ok"); err != nil {
			g.add(cell{agent: s.agent, moment: "first turn", note: err.Error()})
			return
		}
		if !g.offered(s, 60*time.Second) {
			g.add(cell{agent: s.agent, moment: "after first", reply: "not offered", note: "still no model choice after the first message"})
			return
		}
	}
	view, err := g.runtime(s)
	if err != nil {
		g.add(cell{agent: s.agent, moment: "read", offered: true, note: err.Error()})
		return
	}
	if s.agent == "claude" {
		g.claudeLabels(s, view)
	}
	targets := g.targets(s, view)
	moments := []struct {
		name   string
		want   choice
		during string
	}{
		{"new", targets[0], ""},
		{"after first", targets[1], ""},
		// Codex runs a command in the background and answers at once unless
		// told to wait for it (0.4.30 gate: its "while working" turn was over
		// before the pick).
		{"while working", targets[0], "Run this exact shell command and wait until it has finished, then reply with the word done: ping -c 15 127.0.0.1"},
	}
	for _, m := range moments {
		if s.agent == "opencode" && m.name == "new" {
			continue
		}
		g.moment(s, m.name, m.want, m.during)
	}
}

// moment picks want (while a long turn runs, when during is set), then runs
// one turn and reads the agent's own record of it.
func (g *gate) moment(s *session, name string, want choice, during string) {
	cl := cell{agent: s.agent, moment: name, offered: true, want: want}
	if during != "" {
		before := g.evidence(s)
		if _, err := g.c.request(map[string]any{"type": "send_input", "worker_id": s.id, "text": during}, 20*time.Second, "input_sent"); err != nil {
			cl.note = "send: " + err.Error()
			g.add(cl)
			return
		}
		time.Sleep(7 * time.Second)
		// An agent that asks before it runs the command is waiting for an
		// answer: the pick is made then, and the question answered after it,
		// as a person would.
		if len(g.approvals(s)) > 0 || claudeAsks(g.screenText(s)) {
			cl.moment = "waiting for answer"
		}
		if holdsMidTurn(s.agent) && g.evidence(s).turns > before.turns {
			// Not a pick while working: the turn was already over (read by
			// the agents whose record counts a turn at its end).
			cl.note = "the turn ended before the pick"
			g.add(cl)
			return
		}
		cl.reply, cl.pass = g.pick(s, want)
		if cl.pass && holdsMidTurn(s.agent) && !strings.HasPrefix(cl.reply, "pending") {
			// Applied while the turn runs, the pick is not what that turn
			// runs on (0.4.30 gate: Codex acknowledged it and went on on
			// the old model).
			cl.pass, cl.note = false, "applied while the turn ran, not held for its end"
		}
		stop := make(chan struct{})
		go g.approveUntil(s, stop)
		if hasRecord(s.agent) {
			g.waitTurn(s, before.turns)
		} else {
			time.Sleep(30 * time.Second)
		}
		close(stop)
		// A held pick is made as the turn ends: in force within switchLands,
		// or the gate fails (the gate passed a pick that stayed pending).
		if strings.HasPrefix(cl.reply, "pending") {
			ended := time.Now()
			if landed, why := g.lands(s, want); !landed {
				cl.pass, cl.note = false, why
				g.add(cl)
				return
			}
			cl.reply += fmt.Sprintf(", then applied %s after the turn", time.Since(ended).Round(100*time.Millisecond))
		}
	} else {
		cl.reply, cl.pass = g.pick(s, want)
	}
	if !cl.pass {
		g.add(cl)
		return
	}
	if !hasRecord(s.agent) {
		// No reader of this agent's own record yet: the cell stands on the
		// daemon's answer and says so.
		cl.evidence, cl.note = "(not read)", "the agent's own record is not read for "+s.agent
		g.add(cl)
		return
	}
	got, err := g.turn(s, "Reply with exactly the word: ok")
	if err != nil {
		cl.pass, cl.note = false, err.Error()
		g.add(cl)
		return
	}
	cl.evidence = got.String()
	cl.pass = got.matches(s.agent, want)
	g.add(cl)
}

func (g *gate) afterRestart(sessions []*session) {
	out, err := exec.Command("sh", "-c", g.restart).CombinedOutput()
	if err != nil {
		g.add(cell{agent: "daemon", moment: "restart", note: fmt.Sprintf("%v: %s", err, out)})
		return
	}
	var c *client
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)
		if c, err = dial(g.server, g.key); err == nil {
			break
		}
	}
	if c == nil {
		g.add(cell{agent: "daemon", moment: "restart", note: "the daemon did not come back"})
		return
	}
	g.c = c
	for _, s := range sessions {
		if !g.offered(s, 90*time.Second) {
			continue
		}
		view, err := g.runtime(s)
		if err != nil {
			g.add(cell{agent: s.agent, moment: "after restart", note: err.Error()})
			continue
		}
		last := g.evidence(s).last
		truthful := last.model == "" || last.matches(s.agent, view.current)
		g.add(cell{agent: s.agent, moment: "restart label", offered: true, reply: "reads " + view.current.String(), evidence: last.String(), pass: truthful})
		g.moment(s, "after restart", g.targets(s, view)[1], "")
	}
}

// ---- turns and the agents' own records ----

type ran struct {
	model, effort string
}

func (r ran) String() string {
	if r.model == "" {
		return "(no record)"
	}
	if r.effort == "" {
		return r.model
	}
	return r.model + "/" + r.effort
}

// matches says whether a turn's record is the pick: agents record resolved
// model names ("claude-haiku-5-5" for "haiku"), so the pick's own name must
// appear in it.
func (r ran) matches(agent string, want choice) bool {
	model := strings.ToLower(want.model)
	if agent == "opencode" || agent == "pi" || agent == "dsh" {
		if _, id, ok := strings.Cut(model, "/"); ok {
			model = id
		}
	}
	if !strings.Contains(strings.ToLower(r.model), model) {
		return false
	}
	return want.effort == "" || r.effort == "" || strings.EqualFold(r.effort, want.effort)
}

func (c choice) matches(agent string, want choice) bool {
	return ran{c.model, c.effort}.matches(agent, want)
}

type record struct {
	turns int
	last  ran
}

func (g *gate) turn(s *session, prompt string) (ran, error) {
	before := g.evidence(s)
	if _, err := g.c.request(map[string]any{"type": "send_input", "worker_id": s.id, "text": prompt}, 20*time.Second, "input_sent"); err != nil {
		return ran{}, fmt.Errorf("send: %w", err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go g.approveUntil(s, stop)
	after, ok := g.waitTurn(s, before.turns)
	if !ok {
		return after.last, fmt.Errorf("no new turn in the agent's record within %s", g.turnWait)
	}
	return after.last, nil
}

func (g *gate) waitTurn(s *session, turns int) (record, bool) {
	deadline := time.Now().Add(g.turnWait)
	for time.Now().Before(deadline) {
		if now := g.evidence(s); now.turns > turns {
			time.Sleep(2 * time.Second)
			return g.evidence(s), true
		}
		time.Sleep(2 * time.Second)
	}
	return g.evidence(s), false
}

// holdsMidTurn: agents that take a model only between turns, so a pick
// while one works must come back pending.
func holdsMidTurn(agent string) bool {
	return agent == "claude" || agent == "codex"
}

func hasRecord(agent string) bool {
	switch agent {
	case "claude", "codex", "opencode", "pi":
		return true
	}
	return false
}

func (g *gate) evidence(s *session) record {
	switch s.agent {
	case "claude":
		return claudeRecord(s)
	case "codex":
		return codexRecord(s)
	case "opencode":
		return opencodeRecord(s)
	case "pi":
		return piRecord(s)
	}
	return record{}
}

func commandFlag(command, flag string) string {
	fields := strings.Fields(command)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return strings.Trim(fields[i+1], `'"`)
		}
		if v, ok := strings.CutPrefix(f, flag+"="); ok {
			return strings.Trim(v, `'"`)
		}
	}
	return ""
}

// claudeRecord reads Claude's transcript: each answer's model, and Claude's
// own line for the latest switch's effort.
func claudeRecord(s *session) record {
	id := commandFlag(s.command, "--session-id")
	home, _ := os.UserHomeDir()
	slug := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(s.cwd, "-")
	file, err := os.Open(filepath.Join(home, ".claude", "projects", slug, id+".jsonl"))
	if err != nil {
		return record{}
	}
	defer file.Close()
	var r record
	effort := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Model      string          `json:"model"`
				StopReason string          `json:"stop_reason"`
				Content    json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			// A turn is counted at its end: a tool call is the middle of one.
			if line.Message.Model != "" && line.Message.Model != "<synthetic>" && line.Message.StopReason != "tool_use" {
				r.turns++
				r.last = ran{line.Message.Model, effort}
			}
		case "user":
			var text string
			if json.Unmarshal(line.Message.Content, &text) == nil {
				// The daemon's own reader of Claude's answer to /model.
				if answer := work.ParseClaudeSwitch(text); answer.Named != "" && answer.SessionOnly {
					effort = answer.Effort
				}
			}
		}
	}
	return r
}

// codexRecord reads the newest Codex rollout of the Session's folder: each
// finished turn's model and effort.
func codexRecord(s *session) record {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, _ := os.UserHomeDir()
		codexHome = filepath.Join(home, ".codex")
	}
	var files []string
	_ = filepath.Walk(filepath.Join(codexHome, "sessions"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") && info.ModTime().After(s.created) {
			files = append(files, path)
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Stat(files[i])
		b, _ := os.Stat(files[j])
		return a.ModTime().After(b.ModTime())
	})
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if r, mine := codexTurns(raw, s.cwd, s.created); mine {
			return r
		}
	}
	return record{}
}

// codexTurns reads one rollout: whether it is the Session's (its folder,
// started with it), and its turns. A turn is counted at its end (Codex
// writes task_complete, or turn_aborted for a stopped one), on the model and
// effort of its own turn_context: Codex writes turn_context as the turn
// starts, so counting that took a running turn for a finished one (0.4.30
// gate: the next message went into the running turn).
func codexTurns(raw []byte, cwd string, created time.Time) (record, bool) {
	var r record
	mine := false
	turnOf := map[string]ran{}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string `json:"type"`
				Cwd       string `json:"cwd"`
				Model     string `json:"model"`
				Effort    string `json:"effort"`
				Timestamp string `json:"timestamp"`
				TurnID    string `json:"turn_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		switch entry.Type {
		case "session_meta":
			started, _ := time.Parse(time.RFC3339Nano, entry.Payload.Timestamp)
			mine = entry.Payload.Cwd == cwd && !started.Before(created.Add(-time.Minute))
		case "turn_context":
			turnOf[entry.Payload.TurnID] = ran{entry.Payload.Model, entry.Payload.Effort}
		case "event_msg":
			if entry.Payload.Type != "task_complete" && entry.Payload.Type != "turn_aborted" {
				continue
			}
			if turn, ok := turnOf[entry.Payload.TurnID]; ok {
				r.turns++
				r.last = turn
			}
		}
	}
	return r, mine
}

// opencodeRecord asks OpenCode's service for the session's messages: each
// answer's model and variant.
func opencodeRecord(s *session) record {
	id := commandFlag(s.command, "--session")
	if id == "" {
		return record{}
	}
	out, err := exec.Command("opencode", "api", "session.message.list", "--param", "sessionID="+id).Output()
	if err != nil {
		return record{}
	}
	var answer struct {
		Data []struct {
			Model *struct {
				ID       string `json:"id"`
				Provider string `json:"providerID"`
				Variant  string `json:"variant"`
			} `json:"model"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &answer) != nil {
		return record{}
	}
	var r record
	// Newest first.
	for i := len(answer.Data) - 1; i >= 0; i-- {
		if m := answer.Data[i].Model; m != nil {
			r.turns++
			r.last = ran{m.Provider + "/" + m.ID, m.Variant}
		}
	}
	return r
}

// piRecord reads Pi's session file: each answer's model, at the thinking
// level Pi last recorded.
func piRecord(s *session) record {
	path := commandFlag(s.command, "--session")
	raw, err := os.ReadFile(path)
	if err != nil {
		return record{}
	}
	var r record
	thinking := ""
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var entry struct {
			Type          string `json:"type"`
			ThinkingLevel string `json:"thinkingLevel"`
			Message       struct {
				Role     string `json:"role"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		switch {
		case entry.Type == "thinking_level_change":
			thinking = entry.ThinkingLevel
		case entry.Type == "message" && entry.Message.Role == "assistant":
			r.turns++
			r.last = ran{entry.Message.Provider + "/" + entry.Message.Model, thinking}
		}
	}
	return r
}

func (g *gate) table() string {
	var b strings.Builder
	b.WriteString("| agent | moment | offered | pick | daemon | agent's own record | result | note |\n|---|---|---|---|---|---|---|---|\n")
	pass := 0
	for _, cl := range g.cells {
		result := "FAIL"
		if cl.pass {
			result = "pass"
			pass++
		}
		fmt.Fprintf(&b, "| %s | %s | %v | %s | %s | %s | %s | %s |\n", cl.agent, cl.moment, cl.offered, cl.want, cl.reply, cl.evidence, result, strings.ReplaceAll(cl.note, "|", "/"))
	}
	fmt.Fprintf(&b, "\n%d of %d cells pass\n", pass, len(g.cells))
	return b.String()
}
