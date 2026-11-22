package watcher

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// errPromptNotKept: the prompt in flight was not kept on the Session (too
// large, or sent before prompts were kept), so it cannot be sent again.
var errPromptNotKept = errors.New("Mewla kept no copy of the prompt")

// A delegated prompt is in flight from the moment it is prepared in the Turn
// ledger until the agent's first check-in on it. An agent that dies in that
// window has a Turn the ledger knows and the Worker never reported. After the
// relaunch either the conversation holds the prompt (it is accepted as the
// Turn and continued, like any Turn in flight), or it provably does not (the
// prompt is sent again under a new turn id: nothing ran), or it is not known
// (the agent is started again, the prompt is not sent again, and Brain is told
// to decide). This file keeps the prompt and reads that evidence.

// PromptDelivery is whether a dead agent's conversation holds the delegated
// prompt that was in flight when it ended.
type PromptDelivery string

const (
	// PromptHeld: the conversation holds the prompt (its digest is the
	// conversation's newest user message).
	PromptHeld PromptDelivery = "held"
	// PromptNotHeld: the conversation's newest user message is older than the
	// prompt, and Mewla kept the prompt: it is sent again.
	PromptNotHeld PromptDelivery = "not_held"
	// PromptUnknown: neither could be shown, or the prompt was not kept.
	PromptUnknown PromptDelivery = "unknown"
)

// maxKeptPromptBytes keeps a prompt within the session host's per-value limit
// (64 KiB) with room for its record.
const maxKeptPromptBytes = 60 << 10

// keptDelegatedPrompt is the last delegated prompt sent to a Session, kept on
// the Session (hostDataDelegatedPrompt) so it survives a daemon restart.
type keptDelegatedPrompt struct {
	TurnID    string `json:"turn_id"`
	WorkID    string `json:"work_id,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	PurposeID string `json:"purpose_id,omitempty"`
	Payload   string `json:"payload,omitempty"`
	// TooLarge is set, with no payload, for a prompt over maxKeptPromptBytes.
	TooLarge bool      `json:"too_large,omitempty"`
	At       time.Time `json:"at"`
}

// keepDelegatedPrompt keeps payload as the Session's prompt in flight before
// it is sent. A failure to keep it costs only the re-send after a relaunch.
func (w *Watcher) keepDelegatedPrompt(sessionID, payload string, draft delegatedTurnDraft) {
	sessionID = strings.TrimSpace(sessionID)
	if w == nil || sessionID == "" || strings.TrimSpace(draft.ID) == "" {
		return
	}
	kept := keptDelegatedPrompt{
		TurnID: strings.TrimSpace(draft.ID), WorkID: strings.TrimSpace(draft.WorkID),
		Purpose: strings.TrimSpace(draft.Purpose), PurposeID: strings.TrimSpace(draft.PurposeID),
		Payload: payload, At: time.Now().UTC(),
	}
	if len(payload) > maxKeptPromptBytes {
		kept.Payload, kept.TooLarge = "", true
	}
	raw, err := json.Marshal(kept)
	if err == nil {
		err = w.sessions().setRelaunchData(sessionID, hostDataDelegatedPrompt, string(raw))
	}
	if err != nil {
		log.Printf("agent relaunch: keep the prompt of %s on %s: %v", kept.TurnID, sessionID, err)
	}
}

// keptPrompt is the prompt kept for turnID on the Session, if it is the one.
func (w *Watcher) keptPrompt(sessionID, turnID string) (keptDelegatedPrompt, bool) {
	raw, err := w.sessions().relaunchData(sessionID, hostDataDelegatedPrompt)
	if err != nil || strings.TrimSpace(raw) == "" {
		return keptDelegatedPrompt{}, false
	}
	var kept keptDelegatedPrompt
	if json.Unmarshal([]byte(raw), &kept) != nil || kept.TurnID != strings.TrimSpace(turnID) {
		return keptDelegatedPrompt{}, false
	}
	return kept, true
}

// inFlightAdmission is the delegated prompt still in flight on a Session: a
// signal-protocol admission of a Worker (no Brain claim) not yet accepted.
// The newest wins; the lifecycle fences a Work to one prepared admission.
func inFlightAdmission(pending []InputAdmission) (InputAdmission, bool) {
	var found *InputAdmission
	for index := range pending {
		candidate := pending[index]
		if candidate.State != InputAdmissionPending || !candidate.SignalProtocol || candidate.ClaimToken != "" {
			continue
		}
		if found == nil || candidate.AcceptedAt.After(found.AcceptedAt) {
			found = &pending[index]
		}
	}
	if found == nil {
		return InputAdmission{}, false
	}
	return *found, true
}

// deadPromptDelivery reads, from the dead agent's own conversation, whether
// it holds pending. A conversation that cannot be read proves nothing.
func deadPromptDelivery(pending InputAdmission, provider ProviderActivityObservation) PromptDelivery {
	digest := strings.TrimSpace(pending.PayloadSHA256)
	if digest != "" && (strings.TrimSpace(provider.InputSHA256) == digest || strings.TrimSpace(provider.InputUnwrappedSHA256) == digest) {
		return PromptHeld
	}
	if provider.ProbeState != ProbeStateOK || pending.AcceptedAt.IsZero() {
		return PromptUnknown
	}
	// The newest user message began before the prompt was prepared: the
	// conversation holds nothing of it.
	newest := provider.AdmissionAt
	if newest.IsZero() {
		newest = provider.StartedAt
	}
	if strings.TrimSpace(provider.ID) != "" && !newest.IsZero() && newest.Before(pending.AcceptedAt) {
		return PromptNotHeld
	}
	return PromptUnknown
}

// readPromptInFlight finds the delegated prompt in flight on an ended agent
// with no Turn and reads whether its conversation holds it. A held prompt is
// accepted as the Turn (end.TurnID); one provably not held and kept is sent
// again after the relaunch; anything else is unknown.
func (w *Watcher) readPromptInFlight(end *AgentEnd, pending []InputAdmission, provider ProviderActivityObservation) {
	admission, ok := inFlightAdmission(pending)
	if !ok {
		return
	}
	end.PendingTurnID, end.pending = admission.ProposedTurnID, admission
	delivery := deadPromptDelivery(admission, provider)
	switch delivery {
	case PromptHeld:
		if turnID, accepted := w.acceptHeldPrompt(admission, provider, w.agentRelaunch().now()); accepted {
			end.TurnID, end.PendingTurnID = turnID, ""
		} else {
			delivery = PromptUnknown
		}
	case PromptNotHeld:
		if kept, ok := w.keptPrompt(end.SessionID, admission.ProposedTurnID); !ok || kept.TooLarge || kept.Payload == "" || kept.Purpose != "" {
			delivery = PromptUnknown
		}
	}
	end.PromptDelivery = delivery
	log.Printf("agent relaunch: %s ended with prompt %s in flight (sent %s, digest %.12s): %s; its conversation's newest message %q at %s, digest %.12s/%.12s, read %s",
		end.SessionID, admission.ProposedTurnID, admission.AcceptedAt.Format(time.RFC3339), admission.PayloadSHA256, delivery,
		provider.ID, provider.AdmissionAt.Format(time.RFC3339), provider.InputSHA256, provider.InputUnwrappedSHA256, provider.ProbeState)
}

// acceptHeldPrompt accepts the prompt the dead agent's conversation holds as
// its Turn, from the same exact provider admission tuple a live agent's poll
// uses. The live process identity cannot be proven of a dead agent; the
// Session's generation (the pane the prompt was sent to) still must match.
func (w *Watcher) acceptHeldPrompt(pending InputAdmission, provider ProviderActivityObservation, now time.Time) (string, bool) {
	ledger, ok := w.turnLedger.(InputAdmissionLedger)
	if !ok {
		return "", false
	}
	digest := strings.TrimSpace(pending.PayloadSHA256)
	admission := AdmissionFromObservation(provider)
	if strings.TrimSpace(provider.InputUnwrappedSHA256) == digest {
		admission.SHA256 = digest
	}
	if strings.TrimSpace(provider.ID) == "" || admission.Empty() || admission.SHA256 != digest {
		return "", false
	}
	if generation := w.currentPaneGeneration(pending.SessionID); generation != "" && generation != pending.PaneGeneration {
		return "", false
	}
	resolved, err := ledger.ResolveInputAdmission(InputAdmissionResolution{
		SessionID: pending.SessionID, ProposedTurnID: pending.ProposedTurnID,
		Receipt: pending.Receipt, PayloadSHA256: digest,
		ActivityID: strings.TrimSpace(provider.ID), Admission: admission, ResolvedAt: now.UTC(),
	})
	if err != nil {
		log.Printf("agent relaunch: accept the prompt %s its conversation holds on %s: %v", pending.ProposedTurnID, pending.SessionID, err)
		return "", false
	}
	turnID := strings.TrimSpace(resolved.ResolvedTurnID)
	if turnID == "" {
		turnID = pending.ProposedTurnID
	}
	w.ledgerTurnAuthoritative(pending.SessionID, now)
	return turnID, true
}

// resendPrompt sends the prompt the dead agent never read again, under a new
// turn id, once its old admission is aborted. It returns the new turn id and
// the outcome.
func (w *Watcher) resendPrompt(end AgentEnd, plan AgentRelaunchPlan) (turnID, outcome string, err error) {
	supervisor := w.agentRelaunch()
	pending := end.pending
	kept, ok := w.keptPrompt(end.SessionID, pending.ProposedTurnID)
	if !ok || kept.TooLarge || kept.Payload == "" {
		return "", "prompt_unknown", errPromptNotKept
	}
	ledger, ok := w.turnLedger.(InputAdmissionLedger)
	if !ok {
		return "", "prompt_unknown", errPromptNotKept
	}
	if _, err := ledger.AbortInputAdmission(pending.SessionID, pending.ProposedTurnID, pending.Receipt, pending.PayloadSHA256); err != nil {
		return "", "prompt_unknown", err
	}
	turnID = "turn:" + uuid.NewString()
	payload := strings.ReplaceAll(kept.Payload, pending.ProposedTurnID, turnID)
	at := supervisor.now().UTC()
	result, submitErr := supervisor.submit(end.SessionID, plan.Command, payload, plan.WorkID, turnID, at)
	switch out := result.Outcome; {
	case submitErr == nil && out == InputAccepted:
		return turnID, "resent", nil
	case submitErr != nil && InputOutcomeFromError(submitErr) == InputNotSubmitted, submitErr == nil && out == InputNotSubmitted:
		return "", "resend_not_ready", submitErr
	default:
		return turnID, "resend_unknown", submitErr
	}
}

// deliverPromptInFlight finishes a relaunch whose agent ended with a prompt in
// flight it never checked in on: the prompt is sent again when its
// conversation provably does not hold it; otherwise Brain is told, once, that
// the Turn has nothing running and is its to decide.
func (w *Watcher) deliverPromptInFlight(ledger AgentRelaunchLedger, end AgentEnd, plan AgentRelaunchPlan, record *AgentRelaunch) {
	record.FromTurnID = plan.PendingTurnID
	record.Outcome = "prompt_unknown"
	words := plan.Words
	if end.PromptDelivery == PromptNotHeld {
		turnID, outcome, err := w.resendPrompt(end, plan)
		record.TurnID, record.Outcome = turnID, outcome
		if err != nil {
			record.Error = err.Error()
			log.Printf("agent relaunch: send %s again on %s: %v", plan.PendingTurnID, end.SessionID, err)
		}
		switch outcome {
		case "resent", "resend_unknown":
			return
		case "resend_not_ready":
			words = agentStartedWords(plan, end) + ", but it did not become ready, so its task (" + plan.PendingTurnID +
				") was not sent again; nothing is running for it."
		default:
			words = agentStartedWords(plan, end) + "; its task (" + plan.PendingTurnID +
				") had not reached the conversation, but it could not be sent again (" + record.Error + "); nothing is running for it."
		}
	}
	record.Words = words
	report := plan
	report.Words = words
	w.reportAfterRelaunch(ledger, end, report, "Restarted; its task was not resumed")
}

// agentStartedWords: "Claude Code was killed (SIGKILL); Mewla started it
// again in the same conversation".
func agentStartedWords(plan AgentRelaunchPlan, end AgentEnd) string {
	return strings.TrimSpace(plan.Agent+" "+end.Cause()) + "; Mewla started it again in the same conversation"
}
