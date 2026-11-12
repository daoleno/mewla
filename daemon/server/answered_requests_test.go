package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/answered"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// An approval answered from a phone is recorded with that device, reaches
// every connected app at once, is in the history, and sits in its Session's
// thread.
func TestApprovalAnswerIsRecordedWithItsDevice(t *testing.T) {
	f := newBrainWorkFixture(t)
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.srv.SetAnsweredRequests(ledger)
	approvals := &recordingApprovals{pending: []ApprovalWire{{ID: "a1", Kind: "permission", Tool: "Bash", Summary: "git push origin main",
		Options: []ApprovalOptionWire{{ID: "allow", Label: "Allow"}, {ID: "deny", Label: "Deny"}}}}}
	f.srv.SetAgentApprovals(approvals)

	reply := f.request(t, map[string]any{"type": "approval_decision", "request_id": "decide", "worker_id": "%9", "id": "a1", "option_id": "allow"})
	if reply["type"] != "approval_decided" {
		t.Fatalf("reply = %#v", reply)
	}
	broadcast := f.next(t, "request_answered")
	request, _ := broadcast["request"].(map[string]any)
	by, _ := request["by"].(map[string]any)
	if request["id"] != "approval:%9:a1" || request["outcome"] != "approved" || request["answer"] != "Allow" ||
		request["asked"] != "git push origin main" || request["title"] != "Bash" || by["kind"] != "device" || by["device_id"] != "device-work" {
		t.Fatalf("broadcast = %#v", broadcast)
	}

	history := f.request(t, map[string]any{"type": "answered_requests", "request_id": "history"})
	if list, _ := history["requests"].([]any); len(list) != 1 {
		t.Fatalf("history = %#v", history)
	}
	thread := f.srv.withAnsweredRequests("%9", work.CodexConversation{Events: []work.CodexConversationEvent{{ID: "e1", Kind: "assistant_message"}}})
	if len(thread.Events) != 2 || thread.Events[1].Source != "request_answered" || thread.Events[1].Request == nil ||
		thread.Events[1].Request.Anchor.ApprovalID != "a1" || thread.Events[1].ID != "answered:approval:%9:a1" {
		raw, _ := json.Marshal(thread.Events)
		t.Fatalf("thread = %s", raw)
	}
	if other := f.srv.withAnsweredRequests("%10", work.CodexConversation{}); len(other.Events) != 0 {
		t.Fatalf("another Session's thread got %+v", other.Events)
	}
	// A refused answer records nothing.
	approvals.err = &ApprovalDecisionError{Code: "approval_with_brain", Message: "Brain has it."}
	f.request(t, map[string]any{"type": "approval_decision", "request_id": "refused", "worker_id": "%9", "id": "a1", "option_id": "deny"})
	if got, _ := ledger.Get("approval:%9:a1"); got.Outcome != answered.OutcomeApproved {
		t.Fatalf("a refused answer changed the record: %+v", got)
	}
}

// A screen prompt that leaves a live Session's screen with no answer through
// Mewla was answered in its terminal; a flicker, a choice or an ended Session
// records nothing.
func TestPromptLeavingTheScreenIsAnsweredInTheTerminal(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]*classifier.Worker{"%1": {ID: "%1"}, "%2": {ID: "%2"}}
	s := &Server{promptLeftGrace: 20 * time.Millisecond, getWorkerOverride: func(id string) *classifier.Worker { return live[id] }}
	s.answered = ledger
	prompt := permissionPrompt()
	prompt.Title, prompt.Request = "Bash command", "Tip: auto mode handles these prompts\ngit push origin main"
	prompt.Tool, prompt.Summary = "Bash", "git push origin main"
	blocked := func(id string, p *classifier.Prompt) watcher.SessionEvent {
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: id, State: classifier.StateBlocked, Prompt: p}}
	}
	running := func(id string) watcher.SessionEvent {
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: id, State: classifier.StateRunning}}
	}

	// The prompt is first seen before the transcript names its tool call, and
	// read again with it: the record of the answer says what was asked.
	bare := *prompt
	bare.Tool, bare.Summary = "", ""
	s.observePromptLifecycle(blocked("%1", &bare))
	s.observePromptLifecycle(blocked("%1", prompt))
	s.observePromptLifecycle(running("%1"))
	// %2 flickers: gone for one read, back before the grace ends.
	s.observePromptLifecycle(blocked("%2", prompt))
	s.observePromptLifecycle(running("%2"))
	s.observePromptLifecycle(blocked("%2", prompt))
	time.Sleep(100 * time.Millisecond)

	shown := s.PromptAskedAt("%1", "perm1")
	if shown == nil {
		t.Fatal("the prompt that left has no showing")
	}
	got, ok := ledger.Get(promptRecordID("%1", "perm1", shown))
	if !ok || got.By.Kind != answered.ByTerminal || got.Asked != "git push origin main" || got.Tool != "Bash" || got.Summary != "git push origin main" || got.Answer != "" || got.AskedAt == nil {
		t.Fatalf("terminal answer = %+v %v", got, ok)
	}
	if rows := ledger.List(answered.Filter{SessionID: "%2"}); len(rows) != 0 {
		t.Fatalf("a prompt that came back was sealed: %+v", rows)
	}
	// The app's explicit answer, a moment late, replaces the inference.
	explicit := PromptAnswerRecord("%1", *prompt, PromptAnswer{PromptID: "perm1", Number: 1}, nil, answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}, s.PromptAskedAt("%1", "perm1"), time.Now())
	if stood, wrote, _ := ledger.Record(explicit); !wrote || stood.Answer != "Yes" || stood.Outcome != answered.OutcomeApproved || stood.AskedAt == nil {
		t.Fatalf("explicit after terminal = %+v %v", stood, wrote)
	}
	if rows := ledger.List(answered.Filter{SessionID: "%1"}); len(rows) != 1 {
		t.Fatalf("the late answer added a second record: %+v", rows)
	}

	choice := &classifier.Prompt{ID: "c1", Kind: classifier.PromptChoice}
	s.observePromptLifecycle(blocked("%1", choice))
	s.observePromptLifecycle(running("%1"))
	delete(live, "%2")
	s.observePromptLifecycle(running("%2"))
	time.Sleep(100 * time.Millisecond)
	if rows := ledger.List(answered.Filter{SessionID: "%1"}); len(rows) != 1 {
		t.Fatalf("a choice was inferred from the screen: %+v", rows)
	}
	if rows := ledger.List(answered.Filter{SessionID: "%2"}); len(rows) != 0 {
		t.Fatalf("an ended Session's prompt was sealed: %+v", rows)
	}
}

// A Session that asks the same thing twice (the same command, the same
// menu) gets two records: the second answer is not taken for the first.
func TestTheSamePromptAskedTwiceIsAnsweredTwice(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]*classifier.Worker{"%1": {ID: "%1"}}
	s := &Server{promptLeftGrace: 20 * time.Millisecond, getWorkerOverride: func(id string) *classifier.Worker { return live[id] }}
	s.answered = ledger
	prompt := permissionPrompt()
	prompt.Title, prompt.Request = "Bash command", "npm test"
	event := func(p *classifier.Prompt) watcher.SessionEvent {
		state := classifier.StateRunning
		if p != nil {
			state = classifier.StateBlocked
		}
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: "%1", State: state, Prompt: p}}
	}
	answer := func(number int) {
		t.Helper()
		record := PromptAnswerRecord("%1", *prompt, PromptAnswer{PromptID: prompt.ID, Number: number}, nil,
			answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}, s.PromptAskedAt("%1", prompt.ID), time.Now())
		if _, wrote, err := ledger.Record(record); err != nil || !wrote {
			t.Fatalf("answer %d not recorded: %v", number, err)
		}
	}
	s.observePromptLifecycle(event(prompt))
	answer(1)
	s.observePromptLifecycle(event(nil))
	time.Sleep(5 * time.Millisecond)
	s.observePromptLifecycle(event(prompt))
	answer(2)
	s.observePromptLifecycle(event(nil))
	time.Sleep(100 * time.Millisecond)

	rows := ledger.List(answered.Filter{SessionID: "%1"})
	if len(rows) != 2 {
		t.Fatalf("records = %+v", rows)
	}
	answers := map[string]bool{}
	for _, row := range rows {
		answers[row.Answer] = row.By.Kind == answered.ByDevice
	}
	if !answers["Yes"] || !answers["Yes, and don't ask again for this command"] {
		t.Fatalf("answers = %+v", rows)
	}
}

func TestAnswerRecordShapes(t *testing.T) {
	now := time.Date(2026, 11, 12, 13, 40, 0, 0, time.UTC)
	device := answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}
	choicePrompt := classifier.Prompt{ID: "c1", Kind: classifier.PromptChoice, Questions: []classifier.PromptQuestion{
		{Header: "Copies", Question: "Keep both copies?", Options: []classifier.PromptOption{{Number: 1, Label: "Keep both"}, {Number: 2, Label: "Newest"}}},
	}}
	if got := PromptAnswerRecord("%1", choicePrompt, PromptAnswer{Answers: []ChoiceSelection{{Selected: []int{2}}}}, nil, device, nil, now); got.Kind != answered.KindChoice ||
		got.Title != "Copies" || got.Asked != "Keep both copies?" || got.Answer != "Newest" {
		t.Fatalf("choice prompt = %+v", got)
	}
	plan := classifier.Prompt{ID: "p", Kind: classifier.PromptPlan, Title: "Plan", Options: []classifier.PromptOption{{Number: 3, Label: "No, keep planning"}}}
	if got := PromptAnswerRecord("%1", plan, PromptAnswer{Number: 3}, nil, device, nil, now); got.Outcome != answered.OutcomeDenied || got.Answer != "No, keep planning" {
		t.Fatalf("plan = %+v", got)
	}
	event := work.CodexConversationEvent{CallID: "call-1", Timestamp: now.Add(-time.Minute).Format(time.RFC3339Nano),
		Choice: &work.ConversationChoice{Questions: []work.ConversationChoiceQuestion{{Header: "Deploy", Question: "Which region?"}}}}
	if got := ChoiceAnswerRecord("%1", event, []string{"eu-west"}, device, now); got.ID != "choice:%1:call-1" || got.Asked != "Which region?" ||
		got.Answer != "eu-west" || got.AskedAt == nil || got.Anchor.CallID != "call-1" {
		t.Fatalf("transcript choice = %+v", got)
	}
	questionAt := now.Add(-time.Hour)
	item := brain.Work{ID: "w1", Title: "Pick a copy", Question: "Keep both copies, or the newest edit?", QuestionAt: &questionAt}
	if got := WorkQuestionRecord(item, "Keep both", device, now); got.Kind != answered.KindWorkQuestion || got.WorkID != "w1" ||
		got.Asked != item.Question || got.Answer != "Keep both" || got.At() != questionAt {
		t.Fatalf("work question = %+v", got)
	}
	approvalItem := work.DSHInteraction{RPCID: "r1", Payload: json.RawMessage(`{"type":"approval/requested","toolName":"shell","reason":"rm build/","callId":"k1"}`)}
	if got, ok := DSHAnswerRecord("%1", approvalItem, map[string]any{"rpcId": "r1", "outcome": "rejected"}, device, now); !ok ||
		got.Outcome != answered.OutcomeDenied || got.Asked != "rm build/" || got.Anchor.CallID != "k1" {
		t.Fatalf("dsh approval = %+v", got)
	}
	questionItem := work.DSHInteraction{RPCID: "r2", Payload: json.RawMessage(`{"type":"question/requested","questions":[{"question":"Name?","header":"Project"}]}`)}
	if got, ok := DSHAnswerRecord("%1", questionItem, map[string]any{"rpcId": "r2", "cancel": true}, device, now); !ok || got.Outcome != answered.OutcomeDismissed || got.Asked != "Name?" {
		t.Fatalf("dsh dismissed = %+v", got)
	}
}

// Through the real subscription: a Session's answered request is a row of
// its thread snapshot, and a write to the ledger alone (the app's answer
// replacing a terminal one) reaches the open thread as a delta.
func TestAnsweredRequestRowsReachTheSubscribedThread(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		watcher: watcher.New(time.Second),
		providerConversationLoader: func(_ *work.ProviderConversationReader, workerID string) (work.CodexConversation, error) {
			return work.CodexConversation{Available: true, Source: "claude_transcript", SessionID: "conv-1", Events: []work.CodexConversationEvent{
				{ID: "m1", Kind: "assistant_message", Role: "assistant", Body: "Pushing now.", Timestamp: "2026-11-12T13:39:00Z"},
			}}, nil
		},
	}
	srv.answered = ledger
	asked := time.Date(2026, 11, 12, 13, 39, 30, 0, time.UTC)
	terminal := answered.Request{ID: "prompt:provider-agent:p1", Kind: answered.KindPermission, SessionID: "provider-agent",
		Asked: "git push origin main", Outcome: answered.OutcomeAnswered, By: answered.Answerer{Kind: answered.ByTerminal},
		AskedAt: &asked, AnsweredAt: asked.Add(10 * time.Second), Anchor: answered.Anchor{PromptID: "p1"}}
	if _, _, err := ledger.Record(terminal); err != nil {
		t.Fatal(err)
	}
	conn := openThinProxyTestSocket(t, srv)
	if err := conn.WriteJSON(clientMessage{Type: "codex_conversation_subscribe", RequestID: "sub", TargetID: "provider-agent",
		Cwd: "/repo", Command: "claude", StartedAt: json.RawMessage(`"2026-11-12T13:00:00Z"`)}); err != nil {
		t.Fatal(err)
	}
	read := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var message map[string]any
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatalf("no %s: %v", want, err)
			}
			if message["type"] == want {
				return message
			}
		}
	}
	findRow := func(events []any) map[string]any {
		for _, raw := range events {
			if event, _ := raw.(map[string]any); event["id"] == "answered:prompt:provider-agent:p1" {
				return event
			}
		}
		return nil
	}
	snapshot := read("codex_conversation_snapshot")
	conversation, _ := snapshot["conversation"].(map[string]any)
	events, _ := conversation["events"].([]any)
	row := findRow(events)
	if row == nil || row["kind"] != "request_answered" || row["timestamp"] != "2026-11-12T13:39:30Z" || row["call_id"] != nil {
		t.Fatalf("snapshot row = %#v (events %v)", row, events)
	}

	device := terminal
	device.By, device.Answer, device.Outcome = answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone-2"}, "Yes", answered.OutcomeApproved
	if _, wrote, err := ledger.Record(device); err != nil || !wrote {
		t.Fatalf("device record: %v %v", wrote, err)
	}
	delta := read("codex_conversation_delta")
	upserts, _ := delta["upserts"].([]any)
	row = findRow(upserts)
	request, _ := row["request"].(map[string]any)
	by, _ := request["by"].(map[string]any)
	if row == nil || request["outcome"] != "approved" || by["device_id"] != "phone-2" {
		t.Fatalf("delta = %#v", delta)
	}
}

// Brain's thread shows only what was answered in it: a new chat starts with
// none of the old thread's sealed lines.
func TestBrainThreadShowsOnlyItsOwnAnsweredRequests(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{answered: ledger}
	for id, thread := range map[string]string{"prompt:host:p1": "thread-old", "prompt:host:p2": "thread-new", "work:w1:q": "thread-new"} {
		if _, _, err := ledger.Record(answered.Request{ID: id, Kind: answered.KindPermission, SessionID: "host", ThreadID: thread,
			Outcome: answered.OutcomeAnswered, By: answered.Answerer{Kind: answered.ByDevice}}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.brainThreadAnsweredRequests("thread-new")
	if len(got) != 2 {
		t.Fatalf("thread-new rows = %+v", got)
	}
	for _, event := range got {
		if event.Request.ThreadID != "thread-new" {
			t.Fatalf("row from another thread: %+v", event.Request)
		}
	}
}

// A delegated Worker's request is recorded with the Work it asked for, so the
// tray history can name it; a Session that runs no Work names none.
func TestWorkerRequestNamesItsWork(t *testing.T) {
	srv, _ := ledgerNotificationServer(t, "worker:@ask")
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAnsweredRequests(ledger)
	workID, ok := srv.brain.ActiveWorkOfSession("worker:@ask")
	if !ok {
		t.Fatal("the fixture's Worker runs no Work")
	}
	for _, sessionID := range []string{"worker:@ask", "worker:@solo"} {
		srv.recordAnswered(answered.Request{ID: "prompt:" + sessionID + ":p1", Kind: answered.KindPermission, SessionID: sessionID,
			Asked: "Push?", Answer: "Yes", Outcome: answered.OutcomeApproved, By: answered.Answerer{Kind: answered.ByDevice}})
	}
	if got, _ := ledger.Get("prompt:worker:@ask:p1"); got.WorkID != workID {
		t.Fatalf("work_id = %q, want %q", got.WorkID, workID)
	}
	if got, _ := ledger.Get("prompt:worker:@solo:p1"); got.WorkID != "" {
		t.Fatalf("a Session without Work named %q", got.WorkID)
	}
	if rows := ledger.List(answered.Filter{WorkIDs: map[string]bool{workID: true}}); len(rows) != 1 {
		t.Fatalf("the Work's history = %+v", rows)
	}
}

// A Claude permission held by its hook is drawn on the screen too; answered
// through the approval, it is recorded there, and the menu leaving the screen
// afterwards is that same answer, not one given in the terminal.
func TestAPromptAnsweredThroughItsApprovalIsRecordedOnce(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]*classifier.Worker{"%1": {ID: "%1"}}
	s := &Server{promptLeftGrace: 20 * time.Millisecond, getWorkerOverride: func(id string) *classifier.Worker { return live[id] }}
	s.answered = ledger
	prompt := permissionPrompt()
	prompt.Title, prompt.Request = "Bash command", "git push origin main"
	state := func(p *classifier.Prompt) watcher.SessionEvent {
		if p == nil {
			return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: "%1", State: classifier.StateRunning}}
		}
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: "%1", State: classifier.StateBlocked, Prompt: p}}
	}
	s.observePromptLifecycle(state(prompt))
	approval := ApprovalWire{ID: "hook-1", Kind: "exec", Tool: "Bash", Summary: "git push origin main",
		Options: []ApprovalOptionWire{{ID: "allow", Label: "Allow"}, {ID: "deny", Label: "Deny"}}}
	s.recordAnswered(ApprovalAnswerRecord("%1", approval, "allow", answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}, time.Now()))
	s.observePromptLifecycle(state(nil))
	time.Sleep(100 * time.Millisecond)
	rows := ledger.List(answered.Filter{SessionID: "%1"})
	if len(rows) != 1 || rows[0].By.Kind != answered.ByDevice {
		t.Fatalf("records = %+v", rows)
	}

	// The next prompt, with no approval answered while it showed, is still
	// sealed as answered in the terminal.
	time.Sleep(5 * time.Millisecond)
	s.observePromptLifecycle(state(prompt))
	s.observePromptLifecycle(state(nil))
	time.Sleep(100 * time.Millisecond)
	if rows := ledger.List(answered.Filter{SessionID: "%1"}); len(rows) != 2 || rows[0].By.Kind != answered.ByTerminal {
		t.Fatalf("records after a terminal answer = %+v", rows)
	}
}

// A request says what it asked as the agent's own tool call gives it, so a
// sealed line can read "Approved · curl -sI https://example.org" without the
// terminal's text run together.
func TestAnswerRecordsCarryTheToolAndItsLine(t *testing.T) {
	now := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	device := answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}
	prompt := classifier.Prompt{
		ID: "perm1", Kind: classifier.PromptPermission, Title: "Bash command", CallID: "toolu_1",
		Request: "Run shell command\ncurl -sI https://example.org\nThis command requires approval\n\nDo you want to proceed?",
		Tool:    "Bash", Summary: "curl -sI https://example.org",
		Options: []classifier.PromptOption{{Number: 1, Label: "Yes"}, {Number: 4, Label: "No"}},
	}
	got := PromptAnswerRecord("%1", prompt, PromptAnswer{Number: 1}, nil, device, nil, now)
	if got.Tool != "Bash" || got.Summary != "curl -sI https://example.org" || got.Asked != "curl -sI https://example.org" ||
		got.Title != "Bash command" || got.Answer != "Yes" || got.Outcome != answered.OutcomeApproved || got.Anchor.CallID != "" || got.Anchor.PromptID != "perm1" {
		t.Fatalf("permission record = %+v", got)
	}
	// A prompt whose tool call the transcript could not name asks as the screen said.
	prompt.Tool, prompt.Summary, prompt.CallID = "", "", ""
	if bare := PromptAnswerRecord("%1", prompt, PromptAnswer{Number: 4}, nil, device, nil, now); bare.Tool != "" || bare.Summary != "" ||
		bare.Asked != prompt.Request || bare.Outcome != answered.OutcomeDenied {
		t.Fatalf("bare record = %+v", bare)
	}
	approval := ApprovalAnswerRecord("%1", ApprovalWire{ID: "a1", Kind: "permission", Tool: "Bash", Summary: "git push origin main",
		Options: []ApprovalOptionWire{{ID: "allow", Label: "Allow"}}}, "allow", device, now)
	if approval.Tool != "Bash" || approval.Summary != "git push origin main" || approval.Asked != "git push origin main" {
		t.Fatalf("approval record = %+v", approval)
	}
}

// A permission answered in the terminal says how it ended from what the
// provider recorded for the call: it ran, or it was refused. (The transcript
// side, from a real session's records, is in work/claude_tool_call_test.go.)
func TestTerminalAnswerOutcomeComesFromTheToolCall(t *testing.T) {
	call := func(id, command, status string, rejected bool) work.CodexConversationEvent {
		return work.CodexConversationEvent{ID: id, Kind: "command", CallID: id, Command: command, Status: status, Rejected: rejected}
	}
	conversation := work.CodexConversation{Events: []work.CodexConversationEvent{
		call("toolu_ran", "curl -sI https://example.org", "done", false),
		call("toolu_refused", "curl -sI https://example.net", "failed", true),
		call("toolu_failed", "false", "failed", false),
		call("toolu_pending", "sleep 600", "running", false),
	}}
	record := func(summary string) answered.Request {
		return answered.Request{ID: "prompt:%1:p", Kind: "permission", SessionID: "%1", Tool: "Bash", Summary: summary,
			Outcome: answered.OutcomeAnswered, By: answered.Answerer{Kind: answered.ByTerminal}}
	}
	for _, tc := range []struct{ name, callID, summary, want string }{
		{"ran", "toolu_ran", "curl -sI https://example.org", answered.OutcomeApproved},
		{"ran and failed: it was still allowed", "toolu_failed", "false", answered.OutcomeApproved},
		{"refused at the prompt", "toolu_refused", "curl -sI https://example.net", answered.OutcomeDenied},
		{"refused, found by its line (a hook's approval has no call id)", "", "curl  -sI https://example.net", answered.OutcomeDenied},
		{"no result yet: not known", "toolu_pending", "sleep 600", answered.OutcomeAnswered},
		{"not in the transcript: not known", "toolu_other", "curl -sI https://example.net", answered.OutcomeAnswered},
	} {
		if got := settleFromConversation(record(tc.summary), tc.callID, conversation); got.Outcome != tc.want || got.Answer != "" {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
	// An explicit answer is never second-guessed by the transcript.
	explicit := record("curl -sI https://example.net")
	explicit.By.Kind, explicit.Outcome = answered.ByDevice, answered.OutcomeApproved
	if got := settleFromConversation(explicit, "toolu_refused", conversation); got.Outcome != answered.OutcomeApproved {
		t.Fatalf("explicit answer changed: %+v", got)
	}
}

// A permission prompt answered in the terminal also settles the decision the
// agent's hook asked Mewla for it, which no agent event ever clears while the
// call runs or after it was denied: the phone stops asking a question that is
// already answered. A flicker, a choice or an answer through Mewla settles
// nothing.
func TestAPermissionAnsweredInTheTerminalSettlesItsHookApproval(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]*classifier.Worker{"%1": {ID: "%1"}, "%2": {ID: "%2"}, "%3": {ID: "%3"}}
	s := &Server{promptLeftGrace: 20 * time.Millisecond, getWorkerOverride: func(id string) *classifier.Worker { return live[id] }}
	s.answered = ledger
	approvals := &recordingApprovals{}
	s.SetAgentApprovals(approvals)
	prompt := permissionPrompt()
	prompt.Tool, prompt.Summary = "Bash", "ping -c 45 127.0.0.1"
	blocked := func(id string, p *classifier.Prompt) watcher.SessionEvent {
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: id, State: classifier.StateBlocked, Prompt: p}}
	}
	idle := func(id string) watcher.SessionEvent {
		return watcher.SessionEvent{Type: "worker_state_change", Worker: &classifier.Worker{ID: id, State: classifier.StateRunning}}
	}

	s.observePromptLifecycle(blocked("%1", prompt))
	s.observePromptLifecycle(idle("%1"))
	// %2 flickers.
	s.observePromptLifecycle(blocked("%2", prompt))
	s.observePromptLifecycle(idle("%2"))
	s.observePromptLifecycle(blocked("%2", prompt))
	// %3 was answered from the phone while it showed.
	s.observePromptLifecycle(blocked("%3", prompt))
	explicit := PromptAnswerRecord("%3", *prompt, PromptAnswer{PromptID: "perm1", Number: 1}, nil, answered.Answerer{Kind: answered.ByDevice, DeviceID: "phone"}, s.PromptAskedAt("%3", "perm1"), time.Now())
	if _, _, err := ledger.Record(explicit); err != nil {
		t.Fatal(err)
	}
	s.observePromptLifecycle(idle("%3"))
	// A choice is not a permission.
	s.observePromptLifecycle(blocked("%1", &classifier.Prompt{ID: "c1", Kind: classifier.PromptChoice}))
	s.observePromptLifecycle(idle("%1"))
	time.Sleep(100 * time.Millisecond)

	if got := approvals.answeredInTerminal(); len(got) != 1 || got[0] != "%1|Bash|ping -c 45 127.0.0.1" {
		t.Fatalf("hook approvals settled = %q, want only %%1's", got)
	}
}
