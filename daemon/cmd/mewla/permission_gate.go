package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/claude"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/google/uuid"
)

// A delegated Worker starts in bypassPermissions, so it rarely stops at a
// permission prompt. One still opens when an `ask` rule or organization policy
// requires it, or when the executor command chose a mode that can ask (an
// explicit `auto` that fell back to a prompt). Nobody sits at a delegated
// Worker's terminal, so this file is the path those prompts take: Brain is told
// first, as a needs_input on the Worker's turn that carries the action, and
// answers through `mewla worker approve` with the sentence of the brief that
// authorizes it. Brain can hand the question to the user (an approval card in
// the app) and, if nobody answers in time, the prompt is refused with a reason,
// so the Worker is never left waiting at it. Every answer is written to the
// Work.

const (
	routeBrain = "brain"
	routeUser  = "user"

	// answeredByOperator is an answer through the control socket (Brain or a
	// person at a terminal); answeredByApp is one through the app.
	answeredByOperator = "operator"
	answeredByApp      = "app"

	// agentBrainWait is how long Brain holds a blocked action before it goes
	// to the user's phone.
	agentBrainWait = 120 * time.Second

	actionRunes     = 600
	noAnswerMessage = "Nobody answered this request in time, so it was refused. Do not retry it. Carry on with what does not need it, or stop and report that you were blocked and what you needed."
)

// permissionGate is what the gate needs from the rest of the daemon. Every
// field is optional: without delegated, every question goes straight to the
// user as it always did.
type permissionGate struct {
	// delegated reports whether Brain owns the Worker's task and can be told
	// about a question from it.
	delegated func(workerID string) bool
	// tellBrain raises needs_input on the Worker's turn.
	tellBrain func(workerID, summary string, details map[string]any) error
	// resumed clears that needs_input once the question is answered.
	resumed func(workerID, summary string)
	// answered records Brain's answer as the disposition of the review that
	// raised the question.
	answered func(workerID string)
	// audit writes an answer on the Work.
	audit func(workerID, outcome, summary string, details map[string]any)
	// changed tells the app which approvals it may show have changed.
	changed func(workerID string)
}

func (s *agentEventService) isDelegated(workerID string) bool {
	return s.gate.delegated != nil && s.gate.delegated(workerID)
}

func (s *agentEventService) routeOf(approvalID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.routes[approvalID]
}

func (s *agentEventService) setRoute(approvalID, route string) {
	s.mu.Lock()
	s.routes[approvalID] = route
	s.mu.Unlock()
}

// ask publishes a question and waits for its answer. For a delegated Worker
// it goes to Brain first. delegated tells the caller whether nobody is at the
// Worker's terminal, which decides what an unanswered question means.
func (s *agentEventService) ask(ctx context.Context, workerID string, a agentevents.Approval) (decision agentevents.Decision, answered, delegated bool) {
	ctx, cancel := context.WithTimeout(ctx, s.approvalWait)
	defer cancel()
	if s.stop != nil {
		go func() {
			select {
			case <-s.stop:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	delegated = s.isDelegated(workerID)
	route := routeUser
	if delegated && s.gate.tellBrain != nil {
		route = routeBrain
	}
	s.setRoute(a.ID, route)
	defer func() {
		s.mu.Lock()
		delete(s.routes, a.ID)
		if !answered {
			// An answer's own path (decide) still needs these to clear the
			// needs_input it raised, and removes them itself.
			delete(s.raised, a.ID)
			delete(s.told, a.ID)
		}
		s.mu.Unlock()
	}()
	if route == routeBrain {
		summary, details := brainRequest(workerID, a)
		told := make(chan struct{})
		s.mu.Lock()
		s.told[a.ID] = told
		s.mu.Unlock()
		go func() {
			defer close(told)
			s.awaitPublished(ctx, workerID, a.ID)
			if err := s.gate.tellBrain(workerID, summary, details); err != nil {
				log.Printf("permission: Brain could not be told about %s: %v", a.ID, err)
				s.escalate(workerID, a, "Brain could not be told: "+err.Error())
				return
			}
			s.mu.Lock()
			s.raised[a.ID] = true
			s.mu.Unlock()
		}()
		timer := time.AfterFunc(s.brainWait, func() { s.escalate(workerID, a, "Brain did not answer in time") })
		defer timer.Stop()
	}
	decision, answered = s.hub.Ask(ctx, workerID, a, s.now().UTC())
	if !answered && delegated && !s.stopping() {
		s.finishQuestion(workerID, a, "timed_out", "nobody answered in time; the action was refused", "", "")
	}
	return decision, answered, delegated
}

// stopping reports whether the daemon is shutting down. A question that ends
// then was not left unanswered, so nothing is refused or recorded for it.
func (s *agentEventService) stopping() bool {
	if s.stop == nil {
		return false
	}
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// awaitPublished returns once the Hub lists the approval, so Brain is never
// told about a question it cannot yet answer.
func (s *agentEventService) awaitPublished(ctx context.Context, workerID, approvalID string) {
	for ctx.Err() == nil {
		if state, ok := s.hub.State(workerID); ok {
			for _, pending := range state.Approvals {
				if pending.ID == approvalID {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// escalate hands a question Brain holds to the user, once.
func (s *agentEventService) escalate(workerID string, a agentevents.Approval, why string) {
	s.mu.Lock()
	if s.routes[a.ID] != routeBrain {
		s.mu.Unlock()
		return
	}
	s.routes[a.ID] = routeUser
	s.mu.Unlock()
	if s.gate.audit != nil {
		s.gate.audit(workerID, "escalated", fmt.Sprintf("Sent to the user: %s (%s)", describeAction(a), why), actionDetails(workerID, a))
	}
	if s.gate.changed != nil {
		s.gate.changed(workerID)
	}
}

// finishQuestion records how a question ended and clears the needs_input it
// raised. basis is what the answer relied on; by is who answered.
func (s *agentEventService) finishQuestion(workerID string, a agentevents.Approval, outcome, summary, by, basis string) {
	// An answer can arrive while Brain is still being told; clearing the
	// needs_input first would leave it raised for good.
	s.mu.Lock()
	told := s.told[a.ID]
	s.mu.Unlock()
	if told != nil {
		select {
		case <-told:
		case <-time.After(5 * time.Second):
		}
	}
	s.mu.Lock()
	raised := s.raised[a.ID]
	delete(s.raised, a.ID)
	delete(s.told, a.ID)
	s.mu.Unlock()
	if s.gate.audit != nil {
		details := actionDetails(workerID, a)
		details["outcome"] = outcome
		if by != "" {
			details["answered_by"] = by
		}
		if basis != "" {
			details["basis"] = basis
		}
		s.gate.audit(workerID, outcome, summary, details)
	}
	if raised && s.gate.resumed != nil {
		s.gate.resumed(workerID, "Permission question answered: "+outcome)
	}
}

func describeAction(a agentevents.Approval) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s", a.Tool, truncateRunes(a.Summary, actionRunes)))
}

func actionDetails(workerID string, a agentevents.Approval) map[string]any {
	return map[string]any{
		"worker_id":   workerID,
		"approval_id": a.ID,
		"kind":        a.Kind,
		"tool":        a.Tool,
		"action":      truncateRunes(a.Summary, actionRunes),
	}
}

// brainRequest is what Brain reads: the action the Worker waits on and how to
// answer. The Work and its brief are added where the Work is known.
func brainRequest(workerID string, a agentevents.Approval) (string, map[string]any) {
	summary := truncateRunes(fmt.Sprintf("Waiting on a permission prompt: %s %s", a.Tool, strings.Join(strings.Fields(a.Summary), " ")), 150)
	details := actionDetails(workerID, a)
	details["event"] = "permission_blocked"
	details["answer_with"] = fmt.Sprintf(`mewla worker approve -id %s -approval %s -option allow|deny|escalate -basis "<the sentence of the brief that authorizes it>"`, workerID, a.ID)
	details["policy"] = "Approve only what the Work's brief already authorizes, and quote that sentence as -basis. Use escalate for anything the brief does not cover (other people's data, money, accounts or credentials, outside messages, irreversible deletion). Use deny to refuse it. If you do not answer, the user is asked."
	return strings.TrimSpace(summary), details
}

func truncateRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

// askPrompt answers a PermissionRequest hook: a prompt Claude is about to show.
func (s *agentEventService) askPrompt(ctx context.Context, workerID string, payload claude.Payload) control.Response {
	a := claude.Approval(uuid.NewString(), payload)
	decision, answered, delegated := s.ask(ctx, workerID, a)
	if !answered {
		if delegated && !s.stopping() {
			// A delegated Worker has nobody at its terminal: refuse with a
			// reason rather than leave it waiting at a prompt.
			return control.Response{OK: true, HookOutput: claude.DenyPrompt(noAnswerMessage)}
		}
		return control.Response{OK: true}
	}
	if decision.OptionID == "deny" {
		if note := s.takeNote(a.ID); note != "" {
			return control.Response{OK: true, HookOutput: claude.DenyPrompt("Refused: " + note)}
		}
	}
	output, ok := claude.Decision(decision.OptionID)
	if !ok {
		return control.Response{OK: true}
	}
	return control.Response{OK: true, HookOutput: output}
}

func (s *agentEventService) takeNote(approvalID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	note := s.notes[approvalID]
	delete(s.notes, approvalID)
	return note
}
