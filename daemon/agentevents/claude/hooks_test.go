package claude

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func mustMap(t *testing.T, raw string) []agentevents.Event {
	t.Helper()
	p, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return Map("%7", p, now)
}

func TestSessionStartBindsTheConversation(t *testing.T) {
	events := mustMap(t, `{"hook_event_name":"SessionStart","session_id":"s1","transcript_path":"/t/s1.jsonl","source":"startup"}`)
	if len(events) != 1 || events[0].Kind != agentevents.KindBound || events[0].WorkerID != "%7" ||
		events[0].Binding.ConversationID != "s1" || events[0].Binding.Ref != "/t/s1.jsonl" {
		t.Fatalf("events = %+v", events)
	}
}

func TestUserPromptSubmitIsAdmissionAndTurnStartWithoutKeepingTheText(t *testing.T) {
	events := mustMap(t, `{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt_id":"p1","prompt":"Execute: reply ok"}`)
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("Execute: reply ok")))
	if len(events) != 2 || events[0].Kind != agentevents.KindUserMessage || events[0].ID != "p1" ||
		events[0].InputSHA256 != want || events[1].Kind != agentevents.KindTurnStarted || events[1].TurnID != "p1" {
		t.Fatalf("events = %+v", events)
	}
	if strings.Contains(fmt.Sprintf("%+v", events), "reply ok") {
		t.Fatalf("prompt text was kept: %+v", events)
	}
}

func TestStopAndStopFailureEndTheTurnWithStructuredErrors(t *testing.T) {
	done := mustMap(t, `{"hook_event_name":"Stop","prompt_id":"p1"}`)
	if len(done) != 1 || done[0].Kind != agentevents.KindTurnFinished || done[0].Status != agentevents.TurnCompleted || done[0].TurnID != "p1" {
		t.Fatalf("stop = %+v", done)
	}
	for code, class := range map[string]agentevents.ErrorClass{
		"rate_limit": agentevents.ErrorRateLimit, "overloaded": agentevents.ErrorOverloaded,
		"authentication_failed": agentevents.ErrorAuth, "billing_error": agentevents.ErrorBilling,
		"invalid_request": agentevents.ErrorInvalidRequest, "model_not_found": agentevents.ErrorInvalidRequest,
		"max_output_tokens": agentevents.ErrorContext, "server_error": agentevents.ErrorServer,
		"something_new": agentevents.ErrorUnknown,
	} {
		failed := mustMap(t, fmt.Sprintf(`{"hook_event_name":"StopFailure","prompt_id":"p1","error":%q,"error_details":"x"}`, code))
		if len(failed) != 1 || failed[0].Status != agentevents.TurnFailed || failed[0].Error.Class != class || failed[0].Error.Code != code {
			t.Fatalf("%s -> %+v", code, failed)
		}
	}
}

func TestPermissionRequestIsAQuestionNotAnEvent(t *testing.T) {
	raw := `{"hook_event_name":"PermissionRequest","session_id":"s1","prompt_id":"p1","tool_name":"Bash","tool_input":{"command":"touch x","description":"Create x"}}`
	if events := mustMap(t, raw); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
	p, _ := Parse([]byte(raw))
	a := Approval("ap-1", p)
	if a.ID != "ap-1" || a.Tool != "Bash" || a.Summary != "touch x" || len(a.Options) != 2 || a.Options[0].ID != "allow" {
		t.Fatalf("approval = %+v", a)
	}
}

func TestDecisionCarriesOnlyWhatClaudeCanApply(t *testing.T) {
	out, ok := Decision("allow")
	if !ok || !strings.Contains(out, `"behavior":"allow"`) || !strings.Contains(out, `"hookEventName":"PermissionRequest"`) {
		t.Fatalf("allow -> %q %v", out, ok)
	}
	if out, ok := Decision("deny"); !ok || !strings.Contains(out, `"behavior":"deny"`) {
		t.Fatalf("deny -> %q %v", out, ok)
	}
	if _, ok := Decision("always"); ok {
		t.Fatal("an option the hook cannot carry was accepted")
	}
}

func TestParseRejectsPayloadsWithoutAnEvent(t *testing.T) {
	if _, err := Parse([]byte(`{}`)); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Parse([]byte(`nope`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestAutoDeniedCarriesTheActionAndTheRuleTheClassifierNamed(t *testing.T) {
	p, err := Parse([]byte(`{"hook_event_name":"PermissionDenied","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main","description":"push"},"reason":" [Data Exfiltration] "}`))
	if err != nil {
		t.Fatal(err)
	}
	a := AutoDenied("a-1", p)
	if a.Kind != "auto_denied" || a.Tool != "Bash" || a.Summary != "git push origin main" || a.Reason != "[Data Exfiltration]" || a.ConversationID != "s-1" {
		t.Fatalf("approval = %+v", a)
	}
	if len(a.Options) != 2 || a.Options[0].ID != "allow" || a.Options[1].ID != "deny" {
		t.Fatalf("options = %+v", a.Options)
	}
}

func TestDeniedDecisionOnlyLetsTheAgentRetryAnApprovedAction(t *testing.T) {
	out, ok := DeniedDecision("allow")
	if !ok || !strings.Contains(out, `"retry":true`) || !strings.Contains(out, `"hookEventName":"PermissionDenied"`) {
		t.Fatalf("allow = %q, %v", out, ok)
	}
	if out, ok := DeniedDecision("deny"); ok || out != "" {
		t.Fatalf("deny = %q, %v: a refusal that stands prints nothing", out, ok)
	}
}

func TestToolGrantAndDenyPromptAreTheDocumentedHookOutputs(t *testing.T) {
	grant := ToolGrant("approved")
	for _, want := range []string{`"hookEventName":"PreToolUse"`, `"permissionDecision":"allow"`} {
		if !strings.Contains(grant, want) {
			t.Fatalf("grant = %q, missing %s", grant, want)
		}
	}
	deny := DenyPrompt("nobody answered")
	for _, want := range []string{`"hookEventName":"PermissionRequest"`, `"behavior":"deny"`, `"message":"nobody answered"`} {
		if !strings.Contains(deny, want) {
			t.Fatalf("deny = %q, missing %s", deny, want)
		}
	}
}

func TestActionKeyIgnoresFieldsThatVaryBetweenAttempts(t *testing.T) {
	a := ActionKey("Bash", json.RawMessage(`{"command":"git push origin main","description":"first","timeout":1000}`))
	b := ActionKey("Bash", json.RawMessage(`{"timeout":5,"command":" git push origin main ","description":"retry"}`))
	if a != b {
		t.Fatalf("same command keyed differently: %q vs %q", a, b)
	}
	if a == ActionKey("Bash", json.RawMessage(`{"command":"git push origin dev"}`)) {
		t.Fatal("a different command shares a key")
	}
	w1 := ActionKey("Write", json.RawMessage(`{"file_path":"/a","content":"x"}`))
	w2 := ActionKey("Write", json.RawMessage(`{"content":"x","file_path":"/a"}`))
	if w1 != w2 || w1 == ActionKey("Write", json.RawMessage(`{"file_path":"/a","content":"y"}`)) {
		t.Fatalf("non-shell keys: %q %q", w1, w2)
	}
}
