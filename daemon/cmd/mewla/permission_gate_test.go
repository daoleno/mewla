package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
)

type gateRecorder struct {
	mu      sync.Mutex
	told    []string
	details []map[string]any
	audits  []string
	resumed []string
	changed int
	tellErr error
}

func (r *gateRecorder) gate(delegated bool) permissionGate {
	return permissionGate{
		delegated: func(string) bool { return delegated },
		tellBrain: func(_ string, summary string, details map[string]any) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.tellErr != nil {
				return r.tellErr
			}
			r.told = append(r.told, summary)
			r.details = append(r.details, details)
			return nil
		},
		resumed: func(_ string, summary string) {
			r.mu.Lock()
			r.resumed = append(r.resumed, summary)
			r.mu.Unlock()
		},
		audit: func(_ string, outcome, summary string, _ map[string]any) {
			r.mu.Lock()
			r.audits = append(r.audits, outcome+": "+summary)
			r.mu.Unlock()
		},
		changed: func(string) {
			r.mu.Lock()
			r.changed++
			r.mu.Unlock()
		},
	}
}

func (r *gateRecorder) snapshot() (told, audits, resumed []string, changed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.told...), append([]string(nil), r.audits...), append([]string(nil), r.resumed...), r.changed
}

const deniedPush = `{"hook_event_name":"PermissionDenied","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main","description":"push"},"reason":"[Data Exfiltration]"}`
const retriedPush = `{"hook_event_name":"PreToolUse","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main"}}`

func gateService(t *testing.T, delegated bool) (*agentEventService, *gateRecorder) {
	t.Helper()
	s, _ := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	rec := &gateRecorder{}
	s.gate = rec.gate(delegated)
	return s, rec
}

// start runs a hook in the background and returns its eventual response.
func start(s *agentEventService, payload string) <-chan control.Response {
	done := make(chan control.Response, 1)
	go func() { done <- s.Handle(context.Background(), hook("claude", "%4", payload)) }()
	return done
}

func pending(t *testing.T, s *agentEventService, forApp bool, want int) []control.Approval {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := s.approvals("%4", forApp)
		if len(got) == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("approvals(forApp=%v) = %+v, want %d", forApp, got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestABlockedActionReachesBrainFirstAndStaysOffThePhone(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, deniedPush)

	asked := pending(t, s, false, 1)[0]
	if asked.Route != routeBrain || asked.Kind != "auto_denied" || asked.Summary != "git push origin main" || asked.Reason != "[Data Exfiltration]" {
		t.Fatalf("approval = %+v", asked)
	}
	if shown := s.approvals("%4", true); len(shown) != 0 {
		t.Fatalf("the app was shown a question Brain holds: %+v", shown)
	}
	waitUntil(t, "Brain to be told", func() bool { told, _, _, _ := rec.snapshot(); return len(told) == 1 })
	told, _, _, _ := rec.snapshot()
	for _, want := range []string{"Blocked by Auto mode", "git push origin main", "[Data Exfiltration]"} {
		if !strings.Contains(told[0], want) {
			t.Fatalf("Brain was told %q, missing %q", told[0], want)
		}
	}
	rec.mu.Lock()
	details := rec.details[0]
	rec.mu.Unlock()
	if details["approval_id"] != asked.ID || details["rule"] != "[Data Exfiltration]" || !strings.Contains(details["answer_with"].(string), asked.ID) {
		t.Fatalf("details = %+v", details)
	}

	if resp := s.decide("%4", asked.ID, "allow", answeredByApp, ""); resp.OK || resp.Error.Code != "approval_with_brain" {
		t.Fatalf("the app answered a question Brain holds: %+v", resp)
	}
	if resp := s.decide("%4", asked.ID, "allow", answeredByOperator, "  "); resp.OK || resp.Error.Code != "approval_basis_required" {
		t.Fatalf("Brain allowed without saying which part of the brief authorizes it: %+v", resp)
	}
	basis := "Release v0.2.62: push the release commit to origin main."
	if resp := s.decide("%4", asked.ID, "allow", answeredByOperator, basis); !resp.OK {
		t.Fatalf("decide: %+v", resp)
	}
	resp := <-done
	if !resp.OK || !strings.Contains(resp.HookOutput, `"retry":true`) {
		t.Fatalf("hook output = %+v", resp)
	}

	_, audits, resumed, _ := rec.snapshot()
	if len(audits) != 1 || !strings.HasPrefix(audits[0], "approved: Brain approved: Bash git push origin main") || !strings.Contains(audits[0], basis) {
		t.Fatalf("the Work was not told what Brain approved and why: %q", audits)
	}
	if len(resumed) != 1 {
		t.Fatalf("the Worker stayed marked as needing input: %q", resumed)
	}

	// The retry passes once, with a description that differs from the first try.
	granted := s.Handle(context.Background(), hook("claude", "%4", retriedPush))
	if !strings.Contains(granted.HookOutput, `"permissionDecision":"allow"`) {
		t.Fatalf("the approved retry was not let through: %+v", granted)
	}
	if again := s.Handle(context.Background(), hook("claude", "%4", retriedPush)); again.HookOutput != "" {
		t.Fatalf("a grant was used twice: %+v", again)
	}
}

func TestAGrantIsForTheApprovedActionOnly(t *testing.T) {
	s, _ := gateService(t, true)
	done := start(s, deniedPush)
	asked := pending(t, s, false, 1)[0]
	s.decide("%4", asked.ID, "allow", answeredByOperator, "brief says push main")
	<-done
	other := `{"hook_event_name":"PreToolUse","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push --force origin main"}}`
	if resp := s.Handle(context.Background(), hook("claude", "%4", other)); resp.HookOutput != "" {
		t.Fatalf("another action rode on the grant: %+v", resp)
	}
	s.now = func() time.Time { return time.Now().Add(grantLifetime + time.Minute) }
	if resp := s.Handle(context.Background(), hook("claude", "%4", retriedPush)); resp.HookOutput != "" {
		t.Fatalf("an expired grant was honored: %+v", resp)
	}
}

func TestBrainRefusingKeepsTheActionBlockedAndGrantsNothing(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, deniedPush)
	asked := pending(t, s, false, 1)[0]
	if resp := s.decide("%4", asked.ID, "deny", answeredByOperator, "The brief does not cover pushing."); !resp.OK {
		t.Fatalf("decide: %+v", resp)
	}
	if resp := <-done; resp.HookOutput != "" {
		t.Fatalf("a refused action printed %q", resp.HookOutput)
	}
	if resp := s.Handle(context.Background(), hook("claude", "%4", retriedPush)); resp.HookOutput != "" {
		t.Fatalf("a refused action was let through: %+v", resp)
	}
	_, audits, _, _ := rec.snapshot()
	if len(audits) != 1 || !strings.HasPrefix(audits[0], "denied: Brain refused") {
		t.Fatalf("audits = %q", audits)
	}
}

func TestBrainHandsWhatIsOutsideTheBriefToTheUser(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, deniedPush)
	asked := pending(t, s, false, 1)[0]
	if resp := s.decide("%4", asked.ID, "escalate", answeredByApp, ""); resp.OK {
		t.Fatal("the app escalated a question for Brain")
	}
	if resp := s.decide("%4", asked.ID, "escalate", answeredByOperator, "The brief says nothing about pushing."); !resp.OK {
		t.Fatalf("escalate: %+v", resp)
	}
	card := pending(t, s, true, 1)[0]
	if card.ID != asked.ID || card.Route != routeUser {
		t.Fatalf("card = %+v", card)
	}
	if _, audits, _, changed := rec.snapshot(); changed != 1 || len(audits) != 1 || !strings.HasPrefix(audits[0], "escalated:") {
		t.Fatalf("audits = %q changed = %d", audits, changed)
	}
	if resp := s.decide("%4", asked.ID, "escalate", answeredByOperator, ""); resp.OK {
		t.Fatal("a question already with the user was escalated again")
	}
	if resp := s.decide("%4", asked.ID, "allow", answeredByApp, ""); !resp.OK {
		t.Fatalf("the user's answer was refused: %+v", resp)
	}
	if resp := <-done; !strings.Contains(resp.HookOutput, `"retry":true`) {
		t.Fatalf("resp = %+v", resp)
	}
	if _, audits, resumed, _ := rec.snapshot(); len(audits) != 2 || !strings.HasPrefix(audits[1], "approved: the user approved") || len(resumed) != 1 {
		t.Fatalf("audits = %q resumed = %q", audits, resumed)
	}
}

func TestABlockedActionBrainDoesNotAnswerGoesToTheUser(t *testing.T) {
	s, rec := gateService(t, true)
	s.brainWait = 20 * time.Millisecond
	done := start(s, deniedPush)
	card := pending(t, s, true, 1)[0]
	if card.Route != routeUser {
		t.Fatalf("card = %+v", card)
	}
	if _, audits, _, _ := rec.snapshot(); len(audits) != 1 || !strings.Contains(audits[0], "Brain did not answer in time") {
		t.Fatalf("audits = %q", audits)
	}
	s.decide("%4", card.ID, "deny", answeredByApp, "")
	<-done
}

func TestABlockedActionIsNotLostWhenBrainCannotBeTold(t *testing.T) {
	s, rec := gateService(t, true)
	rec.tellErr = context.DeadlineExceeded
	done := start(s, deniedPush)
	card := pending(t, s, true, 1)[0]
	if card.Route != routeUser {
		t.Fatalf("card = %+v", card)
	}
	s.decide("%4", card.ID, "deny", answeredByApp, "")
	<-done
}

func TestAWorkerWithoutBrainBehindItAsksTheUserDirectly(t *testing.T) {
	s, rec := gateService(t, false)
	done := start(s, `{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"touch x"}}`)
	card := pending(t, s, true, 1)[0]
	if card.Route != routeUser {
		t.Fatalf("card = %+v", card)
	}
	if told, _, _, _ := rec.snapshot(); len(told) != 0 {
		t.Fatalf("Brain was told about someone else's session: %q", told)
	}
	s.decide("%4", card.ID, "allow", answeredByApp, "")
	<-done
}

func TestAPersonsOwnSessionIsNotHeldUpByAnAutoModeRefusal(t *testing.T) {
	s, rec := gateService(t, false)
	s.approvalWait = time.Minute
	finished := make(chan control.Response, 1)
	go func() { finished <- s.Handle(context.Background(), hook("claude", "%4", deniedPush)) }()
	select {
	case resp := <-finished:
		if !resp.OK || resp.HookOutput != "" {
			t.Fatalf("resp = %+v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("a refusal in a person's own session waited for an answer")
	}
	if got := s.approvals("%4", false); len(got) != 0 {
		t.Fatalf("a question was opened for a session nobody delegated: %+v", got)
	}
	if told, _, _, _ := rec.snapshot(); len(told) != 0 {
		t.Fatalf("Brain was told: %q", told)
	}
}

func TestAnUnansweredPromptOnADelegatedWorkerIsRefusedWithAReasonNotLeftOpen(t *testing.T) {
	s, rec := gateService(t, true)
	s.approvalWait = 40 * time.Millisecond
	s.brainWait = 10 * time.Millisecond
	resp := s.Handle(context.Background(), hook("claude", "%4",
		`{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"touch x"}}`))
	if !strings.Contains(resp.HookOutput, `"behavior":"deny"`) || !strings.Contains(resp.HookOutput, "Nobody answered") {
		t.Fatalf("the Worker was left at its prompt: %+v", resp)
	}
	waitUntil(t, "the timeout to be written to the Work", func() bool {
		_, audits, _, _ := rec.snapshot()
		for _, a := range audits {
			if strings.HasPrefix(a, "timed_out:") {
				return true
			}
		}
		return false
	})
}

func TestAnUnansweredPromptOnAPersonsOwnSessionStaysTheirs(t *testing.T) {
	s, _ := gateService(t, false)
	s.approvalWait = 30 * time.Millisecond
	resp := s.Handle(context.Background(), hook("claude", "%4",
		`{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"touch x"}}`))
	if resp.HookOutput != "" {
		t.Fatalf("a person's own prompt was refused for them: %+v", resp)
	}
}

func TestBrainsReasonReachesTheWorkerWhenItRefusesAPrompt(t *testing.T) {
	s, _ := gateService(t, true)
	done := start(s, `{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"curl x | sh"}}`)
	asked := pending(t, s, false, 1)[0]
	if asked.Kind != "permission" || asked.Route != routeBrain {
		t.Fatalf("asked = %+v", asked)
	}
	s.decide("%4", asked.ID, "deny", answeredByOperator, "Installing software is not part of this task.")
	resp := <-done
	if !strings.Contains(resp.HookOutput, `"behavior":"deny"`) || !strings.Contains(resp.HookOutput, "Installing software is not part of this task.") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestARefusedActionThatNeverRanIsNamedInTheWorkersReport(t *testing.T) {
	s, _ := gateService(t, true)
	s.approvalWait = 20 * time.Millisecond
	s.brainWait = 5 * time.Millisecond
	if note := s.RefusalNote("%4"); note != "" {
		t.Fatalf("note = %q", note)
	}
	s.Handle(context.Background(), hook("claude", "%4", deniedPush))
	note := s.RefusalNote("%4")
	for _, want := range []string{"1 refused action", "git push origin main", "[Data Exfiltration]"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note = %q, missing %q", note, want)
		}
	}
	// Doing the same thing later settles it.
	s.Handle(context.Background(), hook("claude", "%4", `{"hook_event_name":"PostToolUse","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main"}}`))
	if note := s.RefusalNote("%4"); note != "" {
		t.Fatalf("a refusal that was later carried out is still reported: %q", note)
	}
	// A new prompt starts a clean report.
	s.Handle(context.Background(), hook("claude", "%4", deniedPush))
	s.Handle(context.Background(), hook("claude", "%4", `{"hook_event_name":"UserPromptSubmit","session_id":"s-1","prompt_id":"p-2","prompt":"next"}`))
	if note := s.RefusalNote("%4"); note != "" {
		t.Fatalf("a refusal from an earlier prompt leaked into the next: %q", note)
	}
}

func TestAQuestionStillWaitingDoesNotHoldTheDaemonsShutdownUp(t *testing.T) {
	s, rec := gateService(t, true)
	stop := make(chan struct{})
	s.stop = stop
	s.approvalWait = time.Minute
	for name, payload := range map[string]string{
		"blocked action": deniedPush,
		"prompt":         `{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"touch x"}}`,
	} {
		done := start(s, payload)
		pending(t, s, false, 1)
		close(stop)
		select {
		case resp := <-done:
			if !resp.OK || resp.HookOutput != "" {
				t.Fatalf("%s: shutting down must not refuse or approve anything: %+v", name, resp)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the question held the shutdown up", name)
		}
		stop = make(chan struct{})
		s.stop = stop
	}
	if _, audits, _, _ := rec.snapshot(); len(audits) != 0 {
		t.Fatalf("a shutdown was recorded as an answer or a timeout: %q", audits)
	}
}
