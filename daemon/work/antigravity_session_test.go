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

	"github.com/daoleno/mewla/daemon/platform"
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
	conversation := firstNonEmpty(os.Getenv("MEWLA_FAKE_AGY_CONVERSATION"), fakeAgyConversation)
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
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": conversation, "update": u}})
	}
	setup := map[string]any{
		"sessionId": conversation,
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
		fmt.Fprintf(record, "%s %s %s\n", conversation, message.Method, strings.TrimSpace(string(message.Params)))
		reply := func(result any) {
			send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
		}
		switch message.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "fake", "version": "test"}})
		case "session/new":
			reply(setup)
		case "session/load":
			update(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "from before"}})
			reply(setup)
		case "session/resume":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.SessionID != conversation || os.Getenv("MEWLA_FAKE_AGY_REFUSE_RESUME") == "1" {
				send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32603, "message": "Failed to read session"}})
				continue
			}
			reply(setup)
		case "session/set_model", "session/set_mode", "session/set_config_option":
			if os.Getenv("MEWLA_FAKE_AGY_REFUSE_MODEL") == "1" {
				send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32000, "message": "model unavailable"}})
				continue
			}
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
				refuseFile := os.Getenv("MEWLA_FAKE_AGY_REFUSE_FILE")
				_, refusing := os.Stat(refuseFile)
				switch {
				case strings.Contains(text, "rate limit") || refuseFile != "" && refusing == nil:
					send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "provider refused", "data": map[string]any{"code": "rate_limit", "httpStatus": 429}}})
					return
				case strings.Contains(text, "crash"):
					os.Exit(3)
				case strings.Contains(text, "fruit"):
					update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_q", "name": "ask_question", "title": "Run ask_question", "kind": "other", "status": "pending",
						"args": map[string]any{"questions": []map[string]any{{"question": "Which fruit?", "options": []string{"Apple", "Pear"}}}}})
					update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call_q", "name": "ask_question", "status": "pending",
						"args": map[string]any{"questions": []map[string]any{{"question": "Which fruit?", "options": []string{"Apple", "Pear"}}}}})
				case strings.Contains(text, "background"):
					// agy 1.5.14 with a command left running: the turn ends,
					// then the command finishes and agy answers by itself.
					update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_bg", "name": "run_command", "title": "sleep 1; echo bg", "kind": "execute", "status": "in_progress"})
					update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Running it in the background."}})
					send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 10, "outputTokens": 2, "totalTokens": 12}}})
					time.Sleep(150 * time.Millisecond)
					update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call_bg", "status": "completed", "rawOutput": map[string]string{"output": "bg"}})
					time.Sleep(50 * time.Millisecond)
					update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "It finished."}})
					update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "bg-done"}})
					update(map[string]any{"sessionUpdate": "usage_update", "used": 9442, "size": 1048576})
					return
				case strings.Contains(text, "permission"):
					update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1", "name": "run_command", "title": "echo hi", "kind": "execute", "status": "pending"})
					answer := make(chan string, 1)
					permMu.Lock()
					permissions["p1"] = answer
					permMu.Unlock()
					send(map[string]any{"jsonrpc": "2.0", "id": "p1", "method": "session/request_permission", "params": map[string]any{
						"sessionId": conversation,
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

func shortFakeAgySocket(t *testing.T, dir string) string {
	t.Helper()
	socket := platform.SocketPath(filepath.Join(dir, "events.sock"))
	if err := platform.EnsureSocketDir(socket); err != nil {
		t.Fatal(err)
	}
	return socket
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
	return d.awaitHelloFrom("")
}

// awaitHelloFrom is awaitHello for one Session ("" is any).
func (d *fakeDaemon) awaitHelloFrom(session string) AntigravityLinkMessage {
	d.t.Helper()
	for {
		if hello := d.await("hello"); hello.Conversation != "" && (session == "" || hello.Session == session) {
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
	conversation, err := ParseAntigravitySessionLog(AntigravitySessionLogPath(stateDir, fakeAgyConversation))
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
	turns := AntigravitySessionTurns(mustReadLog(t, AntigravitySessionLogPath(stateDir, fakeAgyConversation)))
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

	// Another run on the same conversation (a Brain host resumed by id) keeps
	// the conversation's log and goes on numbering its turns.
	second := "agy-0b6a3c1e-0000-4000-8000-00000000abce"
	command := "/opt/mewla agy-session --agy-session " + second + " --state-dir " + stateDir + " --conversation " + fakeAgyConversation
	if got := AntigravitySessionLogForCommand(command); got != AntigravitySessionLogPath(stateDir, fakeAgyConversation) {
		t.Fatalf("log of a resumed Session = %q", got)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	in2, typed2 := io.Pipe()
	defer typed2.Close()
	done2 := make(chan error, 1)
	go func() {
		done2 <- RunAntigravitySession(ctx2, AntigravitySessionOptions{ID: second, Cwd: cwd, StateDir: stateDir, Conversation: fakeAgyConversation, Binary: os.Args[0], Socket: socket, In: in2, Out: io.Discard})
	}()
	daemon.awaitHelloFrom(second)
	daemon.send(map[string]string{"t": "send", "id": "r4", "text": "again"})
	if input := daemon.await("input"); input.Turn != fakeAgyConversation+":4" {
		t.Fatalf("turn of the resumed Session = %q", input.Turn)
	}
	daemon.await("turn_end")
	cancel2()
	<-done2
	resumedLog, _ := os.ReadFile(record)
	if strings.Count(string(resumedLog), "session/load") != 0 {
		t.Fatal("a logged conversation was replayed instead of resumed")
	}

	// A conversation Mewla never logged is replayed into its log once.
	fresh := filepath.Join(dir, "fresh")
	third := "agy-0b6a3c1e-0000-4000-8000-00000000abcf"
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	in3, typed3 := io.Pipe()
	defer typed3.Close()
	done3 := make(chan error, 1)
	go func() {
		done3 <- RunAntigravitySession(ctx3, AntigravitySessionOptions{ID: third, Cwd: cwd, StateDir: fresh, Conversation: fakeAgyConversation, Binary: os.Args[0], Socket: socket, In: in3, Out: io.Discard})
	}()
	daemon.awaitHelloFrom(third)
	cancel3()
	<-done3
	imported, _ := os.ReadFile(record)
	if !strings.Contains(string(imported), "session/load {") {
		t.Fatalf("an unlogged conversation was not replayed:\n%s", imported)
	}
}

func TestSupervisedAntigravityDeathUsesOneAgentAttemptAndResumesOriginalConversation(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	record := filepath.Join(dir, "agy-calls")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", record)
	socket := shortFakeAgySocket(t, dir)
	daemon := newFakeDaemon(t, socket)
	id := "agy-0b6a3c1e-0000-4000-8000-00000000dddd"
	run := func(text string) error {
		in, typed := io.Pipe()
		defer typed.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- RunAntigravitySession(ctx, AntigravitySessionOptions{
				ID: id, Cwd: dir, StateDir: stateDir, Model: "model-low", Binary: os.Args[0], Socket: socket,
				In: in, Out: io.Discard, Supervised: true,
			})
		}()
		if hello := daemon.awaitHello(); hello.Conversation != fakeAgyConversation {
			t.Fatalf("conversation changed: %+v", hello)
		}
		daemon.send(map[string]string{"t": "send", "id": text, "text": text})
		if input := daemon.await("input"); input.Turn == "" {
			t.Fatalf("input = %+v", input)
		}
		if text == "crash now" {
			select {
			case err := <-done:
				return err
			case <-time.After(10 * time.Second):
				t.Fatal("supervised bridge retried agy internally")
			}
		}
		if end := daemon.await("turn_end"); end.Stop != "end_turn" {
			t.Fatalf("continuation end = %+v", end)
		}
		cancel()
		return <-done
	}
	if err := run("crash now"); err == nil {
		t.Fatal("agent death did not end supervised bridge")
	}
	calls, _ := os.ReadFile(record)
	if got := strings.Count(string(calls), "initialize "); got != 1 {
		t.Fatalf("one failure made %d agy starts: %s", got, calls)
	}
	records := mustReadLog(t, AntigravitySessionLogPath(stateDir, fakeAgyConversation))
	for _, event := range records {
		if event.T == "turn_end" && event.Turn == fakeAgyConversation+":1" {
			t.Fatal("the bridge settled the task before the outer supervisor could continue it")
		}
		if event.T == "agent_exit" && event.Turn == fakeAgyConversation+":1" {
			t.Fatal("the bridge made the Turn look failed before process supervision ran")
		}
	}
	conversation, err := ParseAntigravitySessionLog(AntigravitySessionLogPath(stateDir, fakeAgyConversation))
	if err != nil || conversation.Activity == nil || conversation.Activity.Status != ProviderActivityRunning {
		t.Fatalf("unfinished task before outer recovery = %+v, %v", conversation.Activity, err)
	}
	_ = run("continue safely")
	calls, _ = os.ReadFile(record)
	if !strings.Contains(string(calls), `session/resume {"cwd":"`+dir+`","mcpServers":[],"sessionId":"`+fakeAgyConversation+`"}`) ||
		strings.Count(string(calls), "session/new ") != 1 || strings.Count(string(calls), "initialize ") != 2 {
		t.Fatalf("outer restart did not make exactly one resumed agy attempt: %s", calls)
	}
	state := loadAntigravitySessionState(antigravitySessionStatePath(stateDir, id))
	if state.Conversation != fakeAgyConversation || state.Model != "model-low" {
		t.Fatalf("original conversation or model lost: %+v", state)
	}
}

func TestSupervisedAntigravityRefusedResumeDoesNotOpenAnotherConversation(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	id := "agy-0b6a3c1e-0000-4000-8000-00000000eeee"
	if err := os.MkdirAll(AntigravitySessionRoot(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAntigravitySessionState(antigravitySessionStatePath(stateDir, id), antigravitySessionState{Conversation: fakeAgyConversation, Model: "model-low"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AntigravitySessionLogPath(stateDir, fakeAgyConversation), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "agy-calls")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", record)
	t.Setenv("MEWLA_FAKE_AGY_REFUSE_RESUME", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in, typed := io.Pipe()
	defer typed.Close()
	err := RunAntigravitySession(ctx, AntigravitySessionOptions{
		ID: id, Cwd: dir, StateDir: stateDir, Binary: os.Args[0], Socket: filepath.Join(dir, "missing.sock"),
		In: in, Out: io.Discard, Supervised: true,
	})
	if err == nil || !strings.Contains(err.Error(), "could not reopen conversation") {
		t.Fatalf("refused resume = %v", err)
	}
	calls, _ := os.ReadFile(record)
	if strings.Count(string(calls), "initialize ") != 1 || strings.Count(string(calls), "session/resume ") != 1 || strings.Contains(string(calls), "session/new ") {
		t.Fatalf("refused resume opened another conversation or retried: %s", calls)
	}
}

func TestSupervisedAntigravityDoesNotContinueOnADifferentModel(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	id := "agy-0b6a3c1e-0000-4000-8000-00000000eeef"
	if err := os.MkdirAll(AntigravitySessionRoot(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAntigravitySessionState(antigravitySessionStatePath(stateDir, id), antigravitySessionState{Conversation: fakeAgyConversation, Model: "model-low"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AntigravitySessionLogPath(stateDir, fakeAgyConversation), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "agy-calls")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", record)
	t.Setenv("MEWLA_FAKE_AGY_REFUSE_MODEL", "1")
	in, typed := io.Pipe()
	defer typed.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := RunAntigravitySession(ctx, AntigravitySessionOptions{
		ID: id, Cwd: dir, StateDir: stateDir, Model: "model-default", Binary: os.Args[0],
		Socket: filepath.Join(dir, "missing.sock"), In: in, Out: io.Discard, Supervised: true,
	})
	if err == nil || !strings.Contains(err.Error(), "could not restore its model") {
		t.Fatalf("failed model restoration = %v", err)
	}
	calls, _ := os.ReadFile(record)
	if strings.Count(string(calls), "initialize ") != 1 || !strings.Contains(string(calls), `session/set_model {"modelId":"model-low"`) || strings.Contains(string(calls), "session/prompt ") || strings.Contains(string(calls), "session/new ") {
		t.Fatalf("a continuation ran on agy's default model: %s", calls)
	}
	state := loadAntigravitySessionState(antigravitySessionStatePath(stateDir, id))
	if state.Conversation != fakeAgyConversation || state.Model != "model-low" {
		t.Fatalf("saved conversation and model changed: %+v", state)
	}
}

func TestPersonalAntigravityModelRestoreUsesItsLocalAttemptLimit(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	id := "agy-0b6a3c1e-0000-4000-8000-00000000eef0"
	if err := os.MkdirAll(AntigravitySessionRoot(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAntigravitySessionState(antigravitySessionStatePath(stateDir, id), antigravitySessionState{Conversation: fakeAgyConversation, Model: "model-low"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AntigravitySessionLogPath(stateDir, fakeAgyConversation), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "agy-calls")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", record)
	t.Setenv("MEWLA_FAKE_AGY_REFUSE_MODEL", "1")
	in, typed := io.Pipe()
	defer typed.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	err := RunAntigravitySession(ctx, AntigravitySessionOptions{
		ID: id, Cwd: dir, StateDir: stateDir, Binary: os.Args[0],
		Socket: filepath.Join(dir, "missing.sock"), In: in, Out: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "agy stopped 3 times") {
		t.Fatalf("local retry budget = %v", err)
	}
	calls, _ := os.ReadFile(record)
	if strings.Count(string(calls), "initialize ") != antigravityRestartLimit || strings.Contains(string(calls), "session/prompt ") {
		t.Fatalf("personal Session attempts or prompt execution = %s", calls)
	}
}

func TestAntigravityStructuredProviderRefusalProjectsToActivity(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", filepath.Join(dir, "agy-calls"))
	socket := shortFakeAgySocket(t, dir)
	daemon := newFakeDaemon(t, socket)
	in, typed := io.Pipe()
	defer typed.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunAntigravitySession(ctx, AntigravitySessionOptions{
			ID: "agy-0b6a3c1e-0000-4000-8000-00000000ffff", Cwd: dir, StateDir: stateDir,
			Binary: os.Args[0], Socket: socket, In: in, Out: io.Discard,
		})
	}()
	daemon.awaitHello()
	daemon.send(map[string]string{"t": "send", "id": "r1", "text": "rate limit now"})
	if end := daemon.await("turn_end"); end.Stop != "error" || end.ErrorCode != "rate_limit" || end.HTTPStatus != 429 {
		t.Fatalf("refusal end = %+v", end)
	}
	conversation, err := ParseAntigravitySessionLog(AntigravitySessionLogPath(stateDir, fakeAgyConversation))
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Activity == nil || conversation.Activity.Error == nil || conversation.Activity.Error.Code != "rate_limit" || conversation.Activity.Error.HTTPStatus != 429 {
		t.Fatalf("provider refusal lost its structured code: %+v", conversation.Activity)
	}
	cancel()
	<-done
}

func TestACPErrorKeepsArbitraryDataFromBreakingTheReply(t *testing.T) {
	var failure acpError
	if err := json.Unmarshal([]byte(`{"code":-32000,"message":"provider refused","data":{"code":429,"httpStatus":429,"details":["retry"]}}`), &failure); err != nil {
		t.Fatal(err)
	}
	code, status := failure.providerDetails()
	if code != "" || status != 429 || failure.Error() != "provider refused" {
		t.Fatalf("error details = %q %d %q", code, status, failure.Error())
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
	// The launch adapter makes the Session program's command before the tools
	// are added; the program takes them as its own flag.
	bridge, ok := AntigravitySessionLaunchCommand("agy --dangerously-skip-permissions", "/opt/mewla", "/state")
	if !ok {
		t.Fatal("a plain agy launch is taken over")
	}
	withTools := WithAgentTools(bridge, "/opt/mewla", "/state", AgentTools{Chrome: true})
	if !strings.HasPrefix(withTools, bridge+" --mcp-config ") || !strings.Contains(withTools, `"mewla_chrome"`) {
		t.Fatalf("the Session program's command with tools = %s", withTools)
	}
	if kept := WithAgentTools("agy -p hi", "/opt/mewla", "/state", AgentTools{Chrome: true}); kept != "agy -p hi" {
		t.Fatalf("print mode got tools it cannot take: %s", kept)
	}
}

func TestAntigravityModelsSetFullReadsFamilyAndLevel(t *testing.T) {
	models := antigravityModels{
		Current: "gemini-4-neon-high", ModelBase: "gemini-4-neon", Effort: "high",
		Bases:   []antigravityChoice{{ID: "gemini-4-neon"}, {ID: "gemini-4-argon"}},
		Efforts: []antigravityChoice{{ID: "low"}, {ID: "medium"}, {ID: "high"}},
	}
	models.setFull("gemini-4-argon-low")
	if models.Current != "gemini-4-argon-low" || models.ModelBase != "gemini-4-argon" || models.Effort != "low" {
		t.Fatalf("full id = %+v", models)
	}
	models.setFull("claude-opus-5-5")
	if models.Current != "claude-opus-5-5" || models.ModelBase != "gemini-4-argon" || models.Effort != "low" {
		t.Fatalf("an id with no family keeps the rest: %+v", models)
	}
}

func TestAntigravitySessionReportsWorkAgyStartsByItself(t *testing.T) {
	previous := antigravityAgentTurnQuiet
	antigravityAgentTurnQuiet = 200 * time.Millisecond
	t.Cleanup(func() { antigravityAgentTurnQuiet = previous })
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", filepath.Join(dir, "agy-calls"))
	socket := filepath.Join(dir, "events.sock")
	daemon := newFakeDaemon(t, socket)
	in, typed := io.Pipe()
	defer typed.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = RunAntigravitySession(ctx, AntigravitySessionOptions{
			ID: "agy-0b6a3c1e-0000-4000-8000-00000000bbbb", Cwd: dir, StateDir: stateDir, Binary: os.Args[0], Socket: socket,
			In: in, Out: io.Discard,
		})
	}()
	// The Session is done writing its files before the folder goes.
	t.Cleanup(func() { cancel(); typed.Close(); <-finished })
	daemon.awaitHello()
	daemon.send(map[string]string{"t": "send", "id": "r1", "text": "run it in the background"})
	if end := daemon.await("turn_end"); end.Turn != fakeAgyConversation+":1" || end.Stop != "end_turn" {
		t.Fatalf("the prompt's turn_end = %+v", end)
	}
	// What agy does after the command finishes is a turn of its own.
	if start := daemon.await("turn_start"); start.Turn != fakeAgyConversation+":2" {
		t.Fatalf("agy's own turn = %+v", start)
	}
	if end := daemon.await("turn_end"); end.Turn != fakeAgyConversation+":2" || end.Stop != "end_turn" {
		t.Fatalf("agy's own turn_end = %+v", end)
	}
	conversation, err := ParseAntigravitySessionLog(AntigravitySessionLogPath(stateDir, fakeAgyConversation))
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Activity == nil || conversation.Activity.ID != fakeAgyConversation+":2" || conversation.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity = %+v", conversation.Activity)
	}
	last := conversation.Events[len(conversation.Events)-1]
	if last.Kind != "assistant_message" || !strings.Contains(last.Body, "bg-done") || last.Partial {
		t.Fatalf("last event = %+v", last)
	}
	// A later message is the next turn.
	daemon.send(map[string]string{"t": "send", "id": "r2", "text": "hello"})
	if input := daemon.await("input"); input.Turn != fakeAgyConversation+":3" {
		t.Fatalf("next input = %+v", input)
	}
}

func TestAntigravitySessionLogShowsAQuestionAnsweredInTheAppOnce(t *testing.T) {
	records := []AntigravityLogRecord{
		{T: "session", At: "2026-11-22T06:07:00Z", Conversation: "c"},
		{T: "user", At: "2026-11-22T06:07:22Z", Turn: "c:1", Text: "Ask me which fruit."},
		{T: "update", At: "2026-11-22T06:07:30Z", Turn: "c:1", Update: json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"q1","name":"ask_question","title":"Run ask_question","status":"pending","args":{"questions":[{"question":"Which fruit?","options":["Apple","Pear"]}]}}`)},
		{T: "update", At: "2026-11-22T06:07:30Z", Turn: "c:1", Update: json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Which fruit? [1] Apple [2] Pear"}}`)},
		{T: "turn_end", At: "2026-11-22T06:07:30Z", Turn: "c:1", Stop: "end_turn"},
		{T: "answer", At: "2026-11-22T06:08:19Z", Ask: &AntigravityAsk{ID: "q-q1"}, Option: "2", By: "mewla"},
		{T: "user", At: "2026-11-22T06:08:19Z", Turn: "c:2", Text: "Pear", Answers: "q-q1"},
		{T: "update", At: "2026-11-22T06:08:19Z", Turn: "c:2", Update: json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"q1","status":"completed"}`)},
		{T: "update", At: "2026-11-22T06:08:23Z", Turn: "c:2", Update: json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"PEAR"}}`)},
		{T: "turn_end", At: "2026-11-22T06:08:23Z", Turn: "c:2", Stop: "end_turn"},
	}
	conversation := projectAntigravitySessionLog("/s/c.jsonl", records)
	var kinds []string
	for _, event := range conversation.Events {
		kinds = append(kinds, event.Kind+":"+event.Body)
		if event.Kind == "tool_call" {
			t.Fatalf("agy's question is its card, not a tool row: %+v", event)
		}
	}
	want := "user_message:Ask me which fruit. assistant_message:Which fruit? [1] Apple [2] Pear assistant_message:PEAR"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("Chat = %s, want %s", got, want)
	}
	if conversation.Activity == nil || conversation.Activity.ID != "c:2" || conversation.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("the answer's turn is the newest: %+v", conversation.Activity)
	}
}

// The Session line says what agy asks, and an answer picked in the app shows
// once, on its card.
func TestAgyQuestionLine(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	t.Setenv("MEWLA_FAKE_AGY_ACP", "1")
	t.Setenv("MEWLA_FAKE_AGY_RECORD", filepath.Join(dir, "agy-calls"))
	socket := filepath.Join(dir, "events.sock")
	daemon := newFakeDaemon(t, socket)
	in, typed := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = RunAntigravitySession(ctx, AntigravitySessionOptions{
			ID: "agy-0b6a3c1e-0000-4000-8000-00000000cccc", Cwd: dir, StateDir: stateDir, Binary: os.Args[0], Socket: socket,
			In: in, Out: io.Discard,
		})
	}()
	t.Cleanup(func() { cancel(); typed.Close(); <-finished })
	daemon.awaitHello()
	daemon.send(map[string]string{"t": "send", "id": "r1", "text": "ask me which fruit"})
	if activity := daemon.await("activity"); activity.Summary != "Which fruit?" || activity.Tool != "ask_question" {
		t.Fatalf("the Session line = %+v", activity)
	}
	ask := daemon.await("ask")
	if ask.Ask == nil || ask.Ask.Kind != "question" || len(ask.Ask.Options) != 2 {
		t.Fatalf("ask = %+v", ask.Ask)
	}
	daemon.await("turn_end")
	daemon.send(map[string]string{"t": "decide", "id": ask.Ask.ID, "option": "2", "by": "mewla"})
	daemon.await("turn_end")
	records, err := ReadAntigravitySessionLog(AntigravitySessionLogPath(stateDir, fakeAgyConversation))
	if err != nil {
		t.Fatal(err)
	}
	var answer *AntigravityLogRecord
	for index := range records {
		if records[index].T == "user" && records[index].Text == "Pear" {
			answer = &records[index]
		}
	}
	if answer == nil || answer.Answers != ask.Ask.ID {
		t.Fatalf("the answer's message names its question: %+v", answer)
	}
	for _, event := range projectAntigravitySessionLog("", records).Events {
		if event.Kind == "user_message" && event.Body == "Pear" {
			t.Fatalf("the answer shows on its card, not as a message: %+v", event)
		}
	}
}
