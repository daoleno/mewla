package codex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// The messages below are recorded from a Codex 0.163.0 app-server driving a
// TUI (worklog/mewla-agent-events/spike-codex-io), trimmed to the fields read.
const (
	thread = "01a168f0-c738-7eb2-bf57-4d8cd87a48b0"
	turn1  = "01a168f1-7af8-7443-b6c4-7741dabc6bd4"
)

var t0 = time.Date(2026, 10, 23, 21, 26, 0, 0, time.UTC)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func kinds(events []agentevents.Event) []agentevents.EventKind {
	out := make([]agentevents.EventKind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

func started(m *Mapper) {
	m.Notification("thread/started", raw(`{"thread":{"id":"`+thread+`","path":"/ch/sessions/rollout-x.jsonl","ephemeral":false,"parentThreadId":null}}`), t0)
}

func TestTheWorkersThreadIsTheFirstRealOneNotAHelper(t *testing.T) {
	m := NewMapper("%4")
	got := m.Notification("thread/started", raw(`{"thread":{"id":"`+thread+`","path":"/ch/sessions/rollout-x.jsonl","ephemeral":false,"parentThreadId":null}}`), t0)
	if len(got) != 1 || got[0].Kind != agentevents.KindBound || got[0].Binding.ConversationID != thread || got[0].Binding.Ref != "/ch/sessions/rollout-x.jsonl" || got[0].Binding.By != "agent" {
		t.Fatalf("bound = %+v", got)
	}
	// The title-generating helper thread that appears with the first turn.
	if got := m.Notification("thread/started", raw(`{"thread":{"id":"01a168f1-194e","ephemeral":true,"parentThreadId":null}}`), t0); got != nil {
		t.Fatalf("a helper thread was taken: %+v", got)
	}
	if got := m.Notification("thread/started", raw(`{"thread":{"id":"side","ephemeral":false,"parentThreadId":"`+thread+`"}}`), t0); got != nil {
		t.Fatalf("a side conversation was taken: %+v", got)
	}
	if m.Thread() != thread {
		t.Fatalf("thread = %q", m.Thread())
	}
}

func TestAFirstTurnWhoseStartWasNeverDeliveredIsNamedByItsFirstItem(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	active := t0.Add(time.Second)
	m.Notification("thread/status/changed", raw(`{"threadId":"`+thread+`","status":{"type":"active","activeFlags":[]}}`), active)
	if !m.Active() || m.Turn() != "" {
		t.Fatal("an active thread with no turn id is when the observer must attach")
	}
	item := t0.Add(2 * time.Second)
	got := m.Notification("item/started", raw(`{"item":{"type":"userMessage","id":"i","clientId":"c-1","content":[{"type":"text","text":"hello"}]},"threadId":"`+thread+`","turnId":"`+turn1+`"}`), item)
	if k := kinds(got); len(k) != 3 || k[0] != agentevents.KindTurnStarted || k[1] != agentevents.KindUserMessage || k[2] != agentevents.KindProgress {
		t.Fatalf("kinds = %v", k)
	}
	if got[0].TurnID != turn1 || !got[0].At.Equal(active) {
		t.Fatalf("the turn started when the thread went active: %+v", got[0])
	}
	if got[1].ClientID != "c-1" || got[1].InputSHA256 == "" || got[1].InputSHA256 != got[1].InputUnwrappedSHA256 {
		t.Fatalf("user message = %+v", got[1])
	}
	// The same turn is not started twice.
	if again := m.Notification("item/completed", raw(`{"item":{"type":"agentMessage","id":"a"},"threadId":"`+thread+`","turnId":"`+turn1+`"}`), item); len(again) != 1 || again[0].Kind != agentevents.KindProgress {
		t.Fatalf("again = %+v", again)
	}
	done := m.Notification("turn/completed", raw(`{"threadId":"`+thread+`","turn":{"id":"`+turn1+`","status":"completed","error":null}}`), item.Add(time.Second))
	if len(done) != 1 || done[0].Kind != agentevents.KindTurnFinished || done[0].Status != agentevents.TurnCompleted || m.Turn() != "" {
		t.Fatalf("done = %+v", done)
	}
}

func TestAnotherThreadsMessagesAreIgnored(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	if got := m.Notification("turn/started", raw(`{"threadId":"01a168f1-194e","turn":{"id":"x"}}`), t0); got != nil {
		t.Fatalf("a helper thread's turn was taken: %+v", got)
	}
	if got := m.Notification("item/started", raw(`{"threadId":"other","turnId":"x","item":{"type":"agentMessage"}}`), t0); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestInterruptedAndFailedTurnsEndAsSuch(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	m.Notification("turn/started", raw(`{"threadId":"`+thread+`","turn":{"id":"t2"}}`), t0)
	got := m.Notification("turn/completed", raw(`{"threadId":"`+thread+`","turn":{"id":"t2","status":"interrupted","error":null}}`), t0)
	if len(got) != 1 || got[0].Status != agentevents.TurnInterrupted {
		t.Fatalf("got %+v", got)
	}
	m.Notification("turn/started", raw(`{"threadId":"`+thread+`","turn":{"id":"t3"}}`), t0)
	// Retries are progress, and carry the error; the end carries it again.
	retry := m.Notification("error", raw(`{"error":{"message":"Reconnecting... 1/5","codexErrorInfo":{"responseStreamDisconnected":{"httpStatusCode":404}}},"willRetry":true,"threadId":"`+thread+`","turnId":"t3"}`), t0)
	if len(retry) != 1 || retry[0].Status != agentevents.TurnFailed || !retry[0].Error.WillRetry || retry[0].Error.Class != agentevents.ErrorInvalidRequest {
		t.Fatalf("retry = %+v", retry)
	}
	if final := m.Notification("error", raw(`{"error":{"message":"x","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":404}}},"willRetry":false,"threadId":"`+thread+`","turnId":"t3"}`), t0); final != nil {
		t.Fatalf("the final error is carried by turn/completed: %+v", final)
	}
	end := m.Notification("turn/completed", raw(`{"threadId":"`+thread+`","turn":{"id":"t3","status":"failed","error":{"message":"unexpected status 404","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":404}}}}}`), t0)
	if len(end) != 1 || end[0].Status != agentevents.TurnFailed || end[0].Error.WillRetry || end[0].Error.HTTPStatus != 404 || end[0].Error.Code != "httpConnectionFailed" {
		t.Fatalf("end = %+v", end)
	}
}

func TestErrorClassesComeFromTheStructuredVariantNotTheText(t *testing.T) {
	for info, want := range map[string]agentevents.ErrorClass{
		`"contextWindowExceeded"`: agentevents.ErrorContext,
		`"usageLimitExceeded"`:    agentevents.ErrorBilling,
		`"serverOverloaded"`:      agentevents.ErrorOverloaded,
		`"unauthorized"`:          agentevents.ErrorAuth,
		`"sandboxError"`:          agentevents.ErrorSandbox,
		`{"httpConnectionFailed":{"httpStatusCode":429}}`:            agentevents.ErrorRateLimit,
		`{"responseStreamDisconnected":{"httpStatusCode":503}}`:      agentevents.ErrorServer,
		`{"responseStreamConnectionFailed":{"httpStatusCode":null}}`: agentevents.ErrorNetwork,
		`{"somethingNew":{}}`: agentevents.ErrorUnknown,
		`"other"`:             agentevents.ErrorUnknown,
	} {
		got := ErrorFrom(raw(`{"message":"rate limit exceeded, auth failed","codexErrorInfo":`+info+`}`), false)
		if got.Class != want {
			t.Errorf("%s: class %s, want %s", info, got.Class, want)
		}
	}
}

func TestACommandApprovalOffersTheAgentsPlainDecisionsAndAnswersThem(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	params := raw(`{"kind":"command","threadId":"` + thread + `","turnId":"` + turn1 + `","itemId":"exec-1","reason":"Allow creating approval_probe.txt?","command":"touch approval_probe.txt","availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["touch"]}},"cancel"]}`)
	approval, ok := m.Approval(raw(`0`), "item/commandExecution/requestApproval", params)
	if !ok || approval.ID != "0" || approval.ConversationID != thread || approval.Kind != "command" || approval.Summary != "touch approval_probe.txt" {
		t.Fatalf("approval = %+v ok=%v", approval, ok)
	}
	if len(approval.Options) != 2 || approval.Options[0].ID != "accept" || approval.Options[1].ID != "cancel" {
		t.Fatalf("options = %+v", approval.Options)
	}
	if _, ok := m.Decision("0", "acceptForSession"); ok {
		t.Fatal("an option the request did not offer was accepted")
	}
	response, ok := m.Decision("0", "accept")
	if !ok || response["decision"] != "accept" {
		t.Fatalf("response = %+v", response)
	}
	if _, ok := m.Decision("0", "accept"); ok {
		t.Fatal("an approval was answered twice")
	}
}

func TestAnApprovalOfAnotherThreadOrAnUnknownKindIsLeftToTheTUI(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	if _, ok := m.Approval(raw(`1`), "item/commandExecution/requestApproval", raw(`{"threadId":"other","command":"x"}`)); ok {
		t.Fatal("another thread's approval was taken")
	}
	if _, ok := m.Approval(raw(`2`), "item/permissions/requestApproval", raw(`{"threadId":"`+thread+`"}`)); ok {
		t.Fatal("a request kind Mewla does not answer was taken")
	}
	file, ok := m.Approval(raw(`"req-9"`), "item/fileChange/requestApproval", raw(`{"threadId":"`+thread+`","reason":"Write to x"}`))
	if !ok || file.ID != "req-9" || len(file.Options) != 4 || file.Kind != "file_change" {
		t.Fatalf("file change = %+v ok=%v", file, ok)
	}
}

func TestServerRequestResolvedClearsTheApproval(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	got := m.Notification("serverRequest/resolved", raw(`{"threadId":"`+thread+`","requestId":0}`), t0)
	if len(got) != 1 || got[0].Kind != agentevents.KindApprovalResolved || got[0].ApprovalID != "0" {
		t.Fatalf("got %+v", got)
	}
}
