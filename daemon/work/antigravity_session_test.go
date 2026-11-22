package work

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary stands in for `agy acp` when MEWLA_FAKE_AGY_ACP is set: a
// minimal ACP agent with one conversation, a permission request, a crash on
// request, and a record of every call it took.
func TestMain(m *testing.M) {
	if os.Getenv("MEWLA_FAKE_AGY_ACP") == "1" && len(os.Args) > 1 && os.Args[1] == "acp" {
		fakeAgyACP()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const fakeAgyConversation = "11111111-2222-4333-8444-000000000001"

func fakeAgyACP() {
	record, _ := os.OpenFile(os.Getenv("MEWLA_FAKE_AGY_RECORD"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	defer record.Close()
	out := json.NewEncoder(os.Stdout)
	var writeMu sync.Mutex
	send := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = out.Encode(v)
	}
	update := func(u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": fakeAgyConversation, "update": u}})
	}
	setup := map[string]any{
		"sessionId": fakeAgyConversation,
		"models":    map[string]any{"currentModelId": "model-default", "availableModels": []map[string]string{{"modelId": "model-default", "name": "Default"}, {"modelId": "model-low", "name": "Low"}}},
		"modes":     map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Default"}, {"id": "plan", "name": "Plan"}}},
	}
	permissions := map[string]chan string{}
	var permMu sync.Mutex
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(line, &message) != nil {
			continue
		}
		if message.Method == "" {
			var key string
			_ = json.Unmarshal(message.ID, &key)
			var result struct {
				Outcome struct {
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			_ = json.Unmarshal(message.Result, &result)
			permMu.Lock()
			if ch := permissions[key]; ch != nil {
				ch <- result.Outcome.OptionID
			}
			permMu.Unlock()
			continue
		}
		fmt.Fprintf(record, "%s %s\n", message.Method, strings.TrimSpace(string(message.Params)))
		reply := func(result any) {
			send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
		}
		switch message.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "fake", "version": "test"}})
		case "session/new":
			reply(setup)
		case "session/resume":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.SessionID != fakeAgyConversation {
				send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32603, "message": "Failed to read session"}})
				continue
			}
			reply(setup)
		case "session/set_model", "session/set_mode", "session/set_config_option":
			reply(map[string]any{})
		case "session/cancel":
		case "session/prompt":
			var params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(message.Params, &params)
			text := ""
			if len(params.Prompt) > 0 {
				text = params.Prompt[0].Text
			}
			id := message.ID
			go func() {
				switch {
				case strings.Contains(text, "crash"):
					os.Exit(3)
				case strings.Contains(text, "permission"):
					update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1", "name": "run_command", "title": "echo hi", "kind": "execute", "status": "pending"})
					answer := make(chan string, 1)
					permMu.Lock()
					permissions["p1"] = answer
					permMu.Unlock()
					send(map[string]any{"jsonrpc": "2.0", "id": "p1", "method": "session/request_permission", "params": map[string]any{
						"sessionId": fakeAgyConversation,
						"toolCall":  map[string]any{"toolCallId": "call_1", "name": "run_command", "title": "echo hi", "kind": "execute"},
						"options":   []map[string]string{{"optionId": "allow", "name": "Allow", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}},
					}})
					chosen := <-answer
					update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call_1", "status": "completed", "rawOutput": map[string]string{"output": "chosen " + chosen}})
				}
				update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "echo: " + text}})
				send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 10, "outputTokens": 2, "totalTokens": 12}}})
			}()
		default:
			send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32601, "message": "no"}})
		}
	}
}

// fakeDaemon is the daemon end of the link, for the test.
type fakeDaemon struct {
	t        *testing.T
	listener net.Listener
	messages chan AntigravityLinkMessage
	mu       sync.Mutex
	conn     net.Conn
}

func newFakeDaemon(t *testing.T, socket string) *fakeDaemon {
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDaemon{t: t, listener: listener, messages: make(chan AntigravityLinkMessage, 256)}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			d.mu.Lock()
			d.conn = conn
			d.mu.Unlock()
			go func() {
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var message AntigravityLinkMessage
					if json.Unmarshal(line, &message) == nil {
						d.messages <- message
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return d
}

func (d *fakeDaemon) send(v any) {
	d.t.Helper()
	raw, _ := json.Marshal(v)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		d.t.Fatal("no Session connected")
	}
	if _, err := d.conn.Write(append(raw, '\n')); err != nil {
		d.t.Fatal(err)
	}
}

// awaitHello returns the first hello that names the conversation (the link
// also says hello as soon as it connects, before agy has started).
func (d *fakeDaemon) awaitHello() AntigravityLinkMessage {
	d.t.Helper()
	for {
		if hello := d.await("hello"); hello.Conversation != "" {
			return hello
		}
	}
}

// await returns the first message of kind t, failing after a while.
func (d *fakeDaemon) await(t string) AntigravityLinkMessage {
	d.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case message := <-d.messages:
			if message.T == t {
				return message
			}
		case <-deadline:
			d.t.Fatalf("no %q from the Session", t)
		}
	}
}

func TestAntigravitySessionRunsAgyOverACP(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cwd := filepath.Join(dir, "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "agy-calls")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", record)
	socket := filepath.Join(dir, "events.sock")
	daemon := newFakeDaemon(t, socket)
	id := "agy-0b6a3c1e-0000-4000-8000-00000000abcd"
	in, typed := io.Pipe()
	defer typed.Close()
	var screen strings.Builder
	var screenMu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunAntigravitySession(ctx, AntigravitySessionOptions{
			ID: id, Cwd: cwd, StateDir: stateDir, Model: "model-low", Binary: os.Args[0], Socket: socket,
			In: in, Out: writerFunc(func(p []byte) (int, error) { screenMu.Lock(); defer screenMu.Unlock(); return screen.Write(p) }),
		})
	}()

	hello := daemon.awaitHello()
	if hello.Conversation != fakeAgyConversation || hello.Session != id {
		t.Fatalf("hello = %+v", hello)
	}

	// A message from Mewla is one turn: acknowledged, recorded, ended with
	// agy's stop reason and usage.
	daemon.send(map[string]string{"t": "send", "id": "r1", "text": "hello"})
	// The message is in agy (input) before the Session acknowledges it.
	input := daemon.await("input")
	if input.ID != "r1" || input.SHA == "" {
		t.Fatalf("input = %+v", input)
	}
	daemon.await("sent")
	end := daemon.await("turn_end")
	if end.Stop != "end_turn" || !strings.Contains(string(end.Usage), `"totalTokens":12`) {
		t.Fatalf("turn_end = %+v", end)
	}

	// A permission is a decision with Mewla's option ids; the answer reaches
	// agy as its own option id.
	daemon.send(map[string]string{"t": "send", "id": "r2", "text": "permission please"})
	ask := daemon.await("ask")
	if ask.Ask == nil || ask.Ask.Kind != "permission" || ask.Ask.Tool != "Command" || ask.Ask.Summary != "echo hi" || len(ask.Ask.Options) != 2 || ask.Ask.Options[0].ID != "allow" || ask.Ask.Options[1].Scope != "deny" {
		t.Fatalf("ask = %+v", ask.Ask)
	}
	daemon.send(map[string]string{"t": "decide", "id": ask.Ask.ID, "option": "allow", "by": "mewla"})
	daemon.await("turn_end")

	// A model change is applied now and kept for every later start.
	daemon.send(map[string]string{"t": "set_model", "req": "m1", "model": "model-default", "mode": "plan"})
	if reply := daemon.await("reply"); !reply.OK {
		t.Fatalf("set_model reply = %+v", reply)
	}

	// agy dies mid-turn: the turn ends failed, agy starts again on the same
	// conversation with the Session's model and mode.
	daemon.send(map[string]string{"t": "send", "id": "r3", "text": "crash now"})
	failed := daemon.await("turn_end")
	if failed.Stop != "error" {
		t.Fatalf("turn after agy died = %+v", failed)
	}
	again := daemon.awaitHello()
	if again.Conversation != fakeAgyConversation {
		t.Fatalf("hello after restart = %+v", again)
	}
	time.Sleep(200 * time.Millisecond)

	raw, _ := os.ReadFile(record)
	calls := string(raw)
	if !strings.Contains(calls, `session/resume {"cwd":"`+cwd+`","mcpServers":[],"sessionId":"`+fakeAgyConversation+`"}`) {
		t.Fatalf("agy was not resumed on the conversation:\n%s", calls)
	}
	if strings.Count(calls, `session/set_model {"modelId":"model-low"`) != 1 || !strings.Contains(calls, `session/set_model {"modelId":"model-default"`) {
		t.Fatalf("model choices not applied as expected:\n%s", calls)
	}
	if last := calls[strings.LastIndex(calls, "session/resume"):]; !strings.Contains(last, `"modelId":"model-default"`) || !strings.Contains(last, `session/set_mode {"modeId":"plan"`) {
		t.Fatalf("the restarted agy did not get the Session's model and mode:\n%s", last)
	}

	// The log is the conversation, in order, with agy's own words.
	conversation, err := ParseAntigravitySessionLog(AntigravitySessionLogPath(stateDir, id))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, event := range conversation.Events {
		kinds = append(kinds, event.Kind+":"+strings.TrimSpace(event.Body))
		if event.Timestamp == "" {
			t.Fatalf("event without a time: %+v", event)
		}
	}
	want := []string{"user_message:hello", "assistant_message:echo: hello", "user_message:permission please", "tool_call:", "assistant_message:echo: permission please", "user_message:crash now"}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Fatalf("conversation = %v, want %v", kinds, want)
	}
	if conversation.Activity == nil || conversation.Activity.Status != ProviderActivityFailed {
		t.Fatalf("activity after agy died = %+v", conversation.Activity)
	}
	if tool := conversation.Events[3]; tool.Status != "completed" || tool.Output != "chosen allow" {
		t.Fatalf("tool call = %+v", tool)
	}
	turns := AntigravitySessionTurns(mustReadLog(t, AntigravitySessionLogPath(stateDir, id)))
	if len(turns) != 3 || turns[0].Usage.TotalTokens != 12 || turns[2].Stop != "error" {
		t.Fatalf("turns = %+v", turns)
	}

	screenMu.Lock()
	shown := screen.String()
	screenMu.Unlock()
	for _, want := range []string{"Antigravity", "echo: hello", "Allow echo hi?", "1. Allow", "starting it again"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("terminal is missing %q:\n%s", want, shown)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the Session did not stop")
	}
}

func mustReadLog(t *testing.T, path string) []AntigravityLogRecord {
	t.Helper()
	records, err := ReadAntigravitySessionLog(path)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestAntigravitySessionLaunchCommand(t *testing.T) {
	launched, ok := AntigravitySessionLaunchCommand(`agy --dangerously-skip-permissions --settings '{"trustedWorkspaces":["/w"]}' --model gemini-4-neon-low --mcp-config '{"mcpServers":{}}'`, "/opt/mewla", "/state")
	if !ok {
		t.Fatal("a plain agy launch was not taken over")
	}
	id := AntigravitySessionID(launched)
	if !IsAntigravitySessionID(id) || AntigravitySessionStateDir(launched) != "/state" {
		t.Fatalf("launch = %s", launched)
	}
	for _, want := range []string{"/opt/mewla agy-session --agy-session " + id, "--model gemini-4-neon-low", "--dangerously-skip-permissions", `--mcp-config '{"mcpServers":{}}'`} {
		if !strings.Contains(launched, want) {
			t.Fatalf("launch %q is missing %q", launched, want)
		}
	}
	if InferWorkerProvider(launched) != WorkerProviderAntigravity {
		t.Fatalf("provider of %q = %q", launched, InferWorkerProvider(launched))
	}
	if shown := ShownCommand(launched); shown != "agy --model gemini-4-neon-low --dangerously-skip-permissions" {
		t.Fatalf("shown = %q", shown)
	}
	// A claude model id on an agy Session is still an agy Session.
	if InferWorkerProvider(strings.Replace(launched, "gemini-4-neon-low", "claude-opus-5-5-low", 1)) != WorkerProviderAntigravity {
		t.Fatal("an agy Session with a Claude model was not an agy Session")
	}
	for _, kept := range []string{"agy -p hello", "agy mcp list", "env X=1 agy", "agy --sandbox", "agy --conversation nope"} {
		if _, ok := AntigravitySessionLaunchCommand(kept, "/opt/mewla", "/state"); ok {
			t.Fatalf("%q was taken over; it must run as agy's own UI", kept)
		}
	}
}

func TestParseACPMCPConfigPassesEnvFromTheSession(t *testing.T) {
	t.Setenv("MEWLA_WORKER_ID", "w-1")
	servers, err := ParseACPMCPConfig(`{"mcpServers":{"mewla_chrome":{"command":"/m","args":["agent-use","chrome-mcp"],"envFrom":["MEWLA_WORKER_ID","NOT_SET_HERE"]}}}`)
	if err != nil || len(servers) != 1 {
		t.Fatalf("servers = %+v, %v", servers, err)
	}
	server := servers[0]
	if server.Name != "mewla_chrome" || server.Command != "/m" || len(server.Args) != 2 || len(server.Env) != 1 || server.Env[0] != (ACPMCPServerEnv{Name: "MEWLA_WORKER_ID", Value: "w-1"}) {
		t.Fatalf("server = %+v", server)
	}
}

func TestWithAgentToolsHandsAgyTheToolsThroughItsSession(t *testing.T) {
	command := WithAgentTools("agy --dangerously-skip-permissions", "/opt/mewla", "/state", AgentTools{Chrome: true})
	if !strings.Contains(command, "--mcp-config") || !strings.Contains(command, `"mewla_chrome"`) || !strings.Contains(command, `"envFrom":["MEWLA_WORKER_ID"]`) {
		t.Fatalf("command = %s", command)
	}
	if _, ok := AntigravitySessionLaunchCommand(command, "/opt/mewla", "/state"); !ok {
		t.Fatal("the launch with tools is no longer taken over")
	}
	if kept := WithAgentTools("agy -p hi", "/opt/mewla", "/state", AgentTools{Chrome: true}); kept != "agy -p hi" {
		t.Fatalf("print mode got tools it cannot take: %s", kept)
	}
}
