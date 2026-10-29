package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/claude"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
)

type gateRecorder struct {
	mu      sync.Mutex
	told    []string
	details []map[string]any
	audits  []string
	resumed []string
	answers int
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
		answered: func(string) {
			r.mu.Lock()
			r.answers++
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

const askedPush = `{"hook_event_name":"PermissionRequest","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main","description":"push"}}`

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

func TestAPermissionPromptReachesBrainFirstAndStaysOffThePhone(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, askedPush)

	asked := pending(t, s, false, 1)[0]
	if asked.Route != routeBrain || asked.Kind != "permission" || asked.Summary != "git push origin main" {
		t.Fatalf("approval = %+v", asked)
	}
	if shown := s.approvals("%4", true); len(shown) != 0 {
		t.Fatalf("the app was shown a question Brain holds: %+v", shown)
	}
	waitUntil(t, "Brain to be told", func() bool { told, _, _, _ := rec.snapshot(); return len(told) == 1 })
	told, _, _, _ := rec.snapshot()
	for _, want := range []string{"Waiting on a permission prompt", "git push origin main"} {
		if !strings.Contains(told[0], want) {
			t.Fatalf("Brain was told %q, missing %q", told[0], want)
		}
	}
	rec.mu.Lock()
	details := rec.details[0]
	rec.mu.Unlock()
	if details["approval_id"] != asked.ID || details["event"] != "prompt_blocked" || !strings.Contains(details["answer_with"].(string), approvalPromptID(asked.ID)) {
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
	if !resp.OK || !strings.Contains(resp.HookOutput, `"behavior":"allow"`) {
		t.Fatalf("hook output = %+v", resp)
	}

	_, audits, resumed, _ := rec.snapshot()
	if len(audits) != 1 || !strings.HasPrefix(audits[0], "approved: Brain approved: Bash git push origin main") || !strings.Contains(audits[0], basis) {
		t.Fatalf("the Work was not told what Brain approved and why: %q", audits)
	}
	if len(resumed) != 1 {
		t.Fatalf("the Worker stayed marked as needing input: %q", resumed)
	}
}

func TestBrainRefusingAPromptDeniesItAndAllowsNothing(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]
	if resp := s.decide("%4", asked.ID, "deny", answeredByOperator, "The brief does not cover pushing."); !resp.OK {
		t.Fatalf("decide: %+v", resp)
	}
	if resp := <-done; strings.Contains(resp.HookOutput, `"behavior":"allow"`) {
		t.Fatalf("a refused action was let through: %q", resp.HookOutput)
	}
	_, audits, _, _ := rec.snapshot()
	if len(audits) != 1 || !strings.HasPrefix(audits[0], "denied: Brain refused") {
		t.Fatalf("audits = %q", audits)
	}
}

func TestBrainHandsWhatIsOutsideTheBriefToTheUser(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, askedPush)
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
	if resp := <-done; !strings.Contains(resp.HookOutput, `"behavior":"allow"`) {
		t.Fatalf("resp = %+v", resp)
	}
	if _, audits, resumed, _ := rec.snapshot(); len(audits) != 2 || !strings.HasPrefix(audits[1], "approved: the user approved") || len(resumed) != 1 {
		t.Fatalf("audits = %q resumed = %q", audits, resumed)
	}
}

func TestABlockedActionBrainDoesNotAnswerGoesToTheUser(t *testing.T) {
	s, rec := gateService(t, true)
	s.brainWait = 20 * time.Millisecond
	done := start(s, askedPush)
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
	done := start(s, askedPush)
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

func TestAQuestionStillWaitingDoesNotHoldTheDaemonsShutdownUp(t *testing.T) {
	s, rec := gateService(t, true)
	stop := make(chan struct{})
	s.stop = stop
	s.approvalWait = time.Minute
	done := start(s, askedPush)
	pending(t, s, false, 1)
	close(stop)
	select {
	case resp := <-done:
		if !resp.OK || resp.HookOutput != "" {
			t.Fatalf("shutting down must not refuse or approve anything: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the question held the shutdown up")
	}
	if _, audits, _, _ := rec.snapshot(); len(audits) != 0 {
		t.Fatalf("a shutdown was recorded as an answer or a timeout: %q", audits)
	}
}

func (r *gateRecorder) answered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answers
}

// Brain's answer is its disposition of the review that raised the question;
// the user's answer, a refused answer and an unanswered question are not.
func TestOnlyBrainsAnswerCountsAsItsDispositionOfThePrompt(t *testing.T) {
	for _, option := range []string{"allow", "deny", "escalate"} {
		t.Run("brain "+option, func(t *testing.T) {
			s, rec := gateService(t, true)
			done := start(s, askedPush)
			asked := pending(t, s, false, 1)[0]
			if resp := s.decide("%4", asked.ID, option, answeredByApp, ""); resp.OK || rec.answered() != 0 {
				t.Fatalf("the app's attempt counted: %+v answers=%d", resp, rec.answered())
			}
			if resp := s.decide("%4", asked.ID, option, answeredByOperator, "Release v0.2.62: push the release commit."); !resp.OK {
				t.Fatalf("decide: %+v", resp)
			}
			if rec.answered() != 1 {
				t.Fatalf("Brain's %s was not recorded as its disposition: %d", option, rec.answered())
			}
			if option == "escalate" {
				// The user's later answer is not Brain's.
				if resp := s.decide("%4", asked.ID, "deny", answeredByApp, ""); !resp.OK {
					t.Fatalf("user answer: %+v", resp)
				}
				if rec.answered() != 1 {
					t.Fatalf("the user's answer counted as Brain's: %d", rec.answered())
				}
			}
			<-done
		})
	}
}

// A Claude started before an upgrade still has the old hook file and keeps
// sending PermissionDenied, which only Auto mode produced.
func TestALegacyPermissionDeniedHookIsAnsweredWithNothing(t *testing.T) {
	s, rec := gateService(t, true)
	resp := s.Handle(context.Background(), hook("claude", "%4",
		`{"hook_event_name":"PermissionDenied","session_id":"s-1","tool_name":"Bash","tool_input":{"command":"git push origin main"},"reason":"[Data Exfiltration]"}`))
	if !resp.OK || resp.HookOutput != "" || resp.Error != nil {
		t.Fatalf("resp = %+v", resp)
	}
	if got := s.approvals("%4", false); len(got) != 0 {
		t.Fatalf("a question was opened: %+v", got)
	}
	if told, _, _, _ := rec.snapshot(); len(told) != 0 {
		t.Fatalf("Brain was told: %q", told)
	}
}

// AskUserQuestion is answered in its own prompt; a hook decision about the tool
// would hand Brain an allow/deny that answers nothing.
func TestAskUserQuestionIsNotAPermissionQuestion(t *testing.T) {
	hub := agentevents.NewHub()
	svc := newAgentEventService(hub, func() []*classifier.Worker { return nil })
	payload := claude.Payload{HookEvent: "PermissionRequest", ToolName: "AskUserQuestion", SessionID: "s"}
	resp := svc.askPrompt(context.Background(), "%1", payload)
	if !resp.OK || resp.HookOutput != "" {
		t.Fatalf("response: %+v", resp)
	}
	if state, ok := hub.State("%1"); ok && len(state.Approvals) != 0 {
		t.Fatalf("an approval was published: %+v", state.Approvals)
	}
}

// A hook-held permission reaches Brain as the same prompt event a dialog in
// the terminal does: numbered options, one answer command, no second name.
func TestAHeldPermissionIsTheOnePromptEvent(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]
	waitUntil(t, "Brain to be told", func() bool { told, _, _, _ := rec.snapshot(); return len(told) == 1 })
	rec.mu.Lock()
	details := rec.details[0]
	rec.mu.Unlock()
	prompt, ok := details["prompt"].(*classifier.Prompt)
	if !ok || prompt.ID != approvalPromptID(asked.ID) || prompt.Kind != classifier.PromptPermission || len(prompt.Options) != 2 ||
		prompt.Options[0].Number != 1 || prompt.Options[0].Label != "Allow" || prompt.Options[1].Number != 2 || prompt.Options[1].Label != "Deny" {
		t.Fatalf("prompt = %+v", details["prompt"])
	}
	answer, _ := details["answer_with"].(string)
	if details["event"] != "prompt_blocked" || !strings.Contains(answer, "mewla worker answer -id %4 -prompt "+prompt.ID+" -choice") || strings.Contains(answer, "approve") {
		t.Fatalf("details = %+v", details)
	}

	app := &controlApp{agentEvents: s}
	if id, found := app.heldApprovalFor("%4", "not-a-prompt"); found {
		t.Fatalf("an unknown prompt matched approval %s", id)
	}
	id, found := app.heldApprovalFor("%4", prompt.ID)
	if !found || id != asked.ID {
		t.Fatalf("held approval for %s = %q, %v", prompt.ID, id, found)
	}
	// Allowing a prompt Brain holds needs the brief sentence, as before.
	if resp := app.answerHeldApproval("%4", id, control.Request{PromptChoice: 1}); resp.OK || resp.Error.Code != "approval_basis_required" {
		t.Fatalf("allowed without a basis: %+v", resp)
	}
	if resp := app.answerHeldApproval("%4", id, control.Request{PromptChoice: 3}); resp.OK || resp.Error.Code != "prompt_refused" {
		t.Fatalf("picked an option the prompt does not have: %+v", resp)
	}
	if resp := app.answerHeldApproval("%4", id, control.Request{PromptChoice: 1, ApprovalBasis: "Release: push to origin main."}); !resp.OK || resp.Confirmation != "answered" {
		t.Fatalf("answer: %+v", resp)
	}
	if resp := <-done; !strings.Contains(resp.HookOutput, `"behavior":"allow"`) {
		t.Fatalf("hook output = %+v", resp)
	}
}

func TestBrainEscalatesAHeldPermissionThroughTheAnswerCommand(t *testing.T) {
	s, rec := gateService(t, true)
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]
	app := &controlApp{agentEvents: s}
	if resp := app.answerHeldApproval("%4", asked.ID, control.Request{PromptEscalate: true, ApprovalBasis: "Pushing is outside the brief."}); !resp.OK {
		t.Fatalf("escalate: %+v", resp)
	}
	if shown := pending(t, s, true, 1); shown[0].Route != routeUser {
		t.Fatalf("the user was not handed the question: %+v", shown)
	}
	_ = rec
	s.decide("%4", asked.ID, "deny", answeredByApp, "")
	<-done
}

// The pane gate may report the dialog before the hook asks. Brain was told
// then, so the hook does not tell it again, and the question stays answerable.
func TestAHookDoesNotTellBrainTwiceWhenThePaneGateWasFirst(t *testing.T) {
	s, rec := gateService(t, true)
	s.gate.paneFiled = func(string) bool { return true }
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]
	time.Sleep(50 * time.Millisecond)
	if told, _, _, _ := rec.snapshot(); len(told) != 0 {
		t.Fatalf("Brain was told twice about one dialog: %q", told)
	}
	if resp := s.decide("%4", asked.ID, "deny", answeredByOperator, "no"); !resp.OK {
		t.Fatalf("decide: %+v", resp)
	}
	<-done
}

func TestTheHubHoldsTheDialogWhileAnApprovalIsPending(t *testing.T) {
	s, _ := gateService(t, true)
	if s.holdsApproval("%4") {
		t.Fatal("held with nothing pending")
	}
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]
	if !s.holdsApproval("%4") {
		t.Fatal("not held while a hook waits")
	}
	s.decide("%4", asked.ID, "deny", answeredByOperator, "no")
	<-done
}
