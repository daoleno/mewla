package watcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A blocking prompt in a delegated Worker's terminal is a fact about its turn,
// not only a colour on its row: nobody sits at that terminal, so Brain must be
// told. The prompt is filed to the ledger as the same blocked progress the
// Worker's own `worker progress --attention user_input` would file, so the
// reducer, the lease and Brain's review all take the one path they already
// have. Clearing it files running again.

// promptGate remembers the prompt filed for one Session, so one prompt is
// filed once and its disappearance is filed once.
type promptGate struct {
	turnID   string
	promptID string
	// filedAt is when the prompt was last filed or its lease renewed.
	filedAt time.Time
	// permission is whether the filed prompt is a tool permission dialog.
	permission bool
}

const (
	promptGateLeaseSeconds = 900
	// promptGateRenewEvery is how often a prompt still open renews the turn's
	// lease. The lifecycle sweep expires a lease whether or not the turn is
	// blocked and gives up on it after a grace period, so a prompt left
	// unanswered would otherwise decay to a lost turn.
	promptGateRenewEvery  = 5 * time.Minute
	promptGateAnswerLease = 300
)

// ErrPromptGone means the prompt an answer was composed for is no longer the
// one on screen. No key was sent.
var ErrPromptGone = errors.New("the prompt has passed")

// withChoicePrompt gives an AskUserQuestion signal its structured prompt. The
// pane shows one question at a time, so the other tabs carry only their header
// until the transcript fills them.
func withChoicePrompt(signal classifier.ActivitySignal, pane string) classifier.ActivitySignal {
	if signal.State != classifier.StateBlocked || signal.Source != "claude_pane_choice" {
		return signal
	}
	screen := parseClaudeChoiceScreen(pane)
	if screen.Kind != claudeChoiceScreenQuestion && screen.Kind != claudeChoiceScreenReview {
		return signal
	}
	prompt := classifier.Prompt{Kind: classifier.PromptChoice, Provider: signal.Provider, Title: signal.Summary}
	if screen.Kind == claudeChoiceScreenReview {
		prompt.ID = classifier.PromptID(classifier.PromptChoice, append([]string{"review"}, screen.Tabs...)...)
		for index, header := range screen.Tabs {
			prompt.Questions = append(prompt.Questions, classifier.PromptQuestion{Header: header, Answered: index < len(screen.TabsAnswered) && screen.TabsAnswered[index]})
		}
		signal.Prompt = &prompt
		return signal
	}
	current := classifier.PromptQuestion{Question: screen.Question}
	for _, row := range screen.Rows {
		current.MultiSelect = current.MultiSelect || row.HasBox
		current.Options = append(current.Options, classifier.PromptOption{Number: row.Number, Label: row.Text, Selected: row.Checked || row.Cursor})
	}
	if len(screen.Tabs) <= 1 {
		prompt.Questions = []classifier.PromptQuestion{current}
		parts := []string{current.Question}
		for _, option := range current.Options {
			parts = append(parts, option.Label)
		}
		prompt.ID = classifier.PromptID(classifier.PromptChoice, parts...)
	} else {
		for index, header := range screen.Tabs {
			question := classifier.PromptQuestion{Header: header, Answered: index < len(screen.TabsAnswered) && screen.TabsAnswered[index]}
			prompt.Questions = append(prompt.Questions, question)
		}
		// The tab list is stable while the questions are answered one by one.
		prompt.ID = classifier.PromptID(classifier.PromptChoice, screen.Tabs...)
	}
	signal.Prompt = &prompt
	return signal
}

// PromptEnricher completes a choice prompt from the provider transcript: every
// question with its options, and the tool call behind it. It must keep the
// prompt's ID.
type PromptEnricher func(sessionID string, prompt classifier.Prompt) classifier.Prompt

// promptEnrichment caches one Session's enriched choice prompt, so the
// transcript is read once per prompt and not on every poll.
type promptEnrichment struct {
	mu      sync.Mutex
	enrich  PromptEnricher
	entries map[string]enrichedPrompt
}

type enrichedPrompt struct {
	promptID  string
	questions []classifier.PromptQuestion
	callID    string
	triedAt   time.Time
}

// promptEnrichRetry spaces the transcript reads for a prompt the transcript
// could not yet complete.
const promptEnrichRetry = 2 * time.Second

// SetPromptEnricher installs where a choice prompt gets the questions the
// pane shows one tab at a time.
func (w *Watcher) SetPromptEnricher(enrich PromptEnricher) {
	if w == nil {
		return
	}
	w.promptEnrich.mu.Lock()
	w.promptEnrich.enrich = enrich
	w.promptEnrich.entries = nil
	w.promptEnrich.mu.Unlock()
}

// enrichChoicePrompt gives a choice prompt every question of its tool call.
// The pane stays the authority on which tabs are answered.
func (w *Watcher) enrichChoicePrompt(sessionID string, signal classifier.ActivitySignal) classifier.ActivitySignal {
	prompt := signal.Prompt
	if prompt == nil || prompt.Kind != classifier.PromptChoice {
		return signal
	}
	state := &w.promptEnrich
	state.mu.Lock()
	enrich := state.enrich
	entry, cached := state.entries[sessionID]
	state.mu.Unlock()
	if enrich == nil {
		return signal
	}
	if !cached || entry.promptID != prompt.ID {
		entry = enrichedPrompt{promptID: prompt.ID}
		cached = false
	}
	if !cached || (len(entry.questions) == 0 && time.Since(entry.triedAt) >= promptEnrichRetry) {
		enriched := enrich(sessionID, *prompt)
		entry = enrichedPrompt{promptID: prompt.ID, triedAt: time.Now()}
		if enriched.CallID != "" {
			// Only a prompt tied to its tool call is the transcript's.
			entry.questions, entry.callID = enriched.Questions, enriched.CallID
		}
		state.mu.Lock()
		if state.entries == nil {
			state.entries = map[string]enrichedPrompt{}
		}
		state.entries[sessionID] = entry
		state.mu.Unlock()
	}
	if len(entry.questions) == 0 {
		return signal
	}
	merged := *prompt
	merged.CallID = entry.callID
	merged.Questions = make([]classifier.PromptQuestion, len(entry.questions))
	for index, question := range entry.questions {
		if index < len(prompt.Questions) && len(prompt.Questions) > 1 {
			question.Answered = prompt.Questions[index].Answered
		}
		merged.Questions[index] = question
	}
	signal.Prompt = &merged
	return signal
}

// syncPromptGate files the prompt open in a delegated Worker's pane to its
// turn, and files its disappearance. A Worker can reach a prompt before its
// first progress report has admitted its turn; the pending prompt-carried turn
// is then promoted by this very fact, as it is by the Worker's own report. It
// returns the turn as the ledger now holds it.
func (w *Watcher) syncPromptGate(id string, worker classifier.Worker, alive bool, activity classifier.ActivitySignal, turn TurnSnapshot, hasTurn bool, pending []InputAdmission) (TurnSnapshot, bool) {
	if !worker.Delegated || worker.Hidden || !alive {
		return turn, hasTurn
	}
	turnID := ""
	if hasTurn && turn.SignalProtocol && !TurnTerminal(turn.Status) {
		turnID = turn.TurnID
	} else {
		var newest *InputAdmission
		for index := range pending {
			candidate := pending[index]
			if candidate.State == InputAdmissionPending && candidate.SignalProtocol && candidate.ClaimToken == "" && candidate.Purpose == "" &&
				(newest == nil || candidate.AcceptedAt.After(newest.AcceptedAt)) {
				newest = &pending[index]
			}
		}
		if newest != nil {
			turnID = newest.ProposedTurnID
			turn = TurnSnapshot{SessionID: id, TurnID: turnID, Status: TurnAdmitted, SignalProtocol: true}
		}
	}
	if turnID == "" {
		return turn, hasTurn
	}
	w.mu.RLock()
	gate, filed := w.gates[id]
	w.mu.RUnlock()
	open := activity.State == classifier.StateBlocked
	promptID := ""
	if activity.Prompt != nil {
		promptID = activity.Prompt.ID
	}
	renewing := false
	if filed && gate.turnID == turnID && open && gate.promptID == promptID {
		if time.Since(gate.filedAt) < promptGateRenewEvery {
			return turn, hasTurn
		}
		// Still the same prompt: renew the lease below. The filed gate stays
		// in place so the prompt is not read as new.
		filed, renewing = false, true
	}
	refresh := func() {
		if refreshed, ok, err := w.ledgerTurnAuthoritative(id, time.Now().UTC()); ok && err == nil && refreshed.TurnID == turnID {
			turn, hasTurn = refreshed, true
		}
	}
	if filed {
		w.mu.Lock()
		delete(w.gates, id)
		w.mu.Unlock()
		if gate.turnID == turnID && turn.Status == TurnBlocked {
			if _, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{
				TurnID: turnID, Status: "running", Phase: "working", Attention: "none",
				Summary: "The prompt was answered", EventKind: "progress", LeaseSeconds: promptGateAnswerLease,
				ProgressEventID: fmt.Sprintf("pane-gate-clear:%s:%s:%d", turnID, gate.promptID, time.Now().UnixNano()),
			}); err != nil {
				return turn, hasTurn
			}
			refresh()
		}
	}
	// A turn already blocked by the Worker's own report or the permission hook
	// has told Brain; this prompt is that one. A permission dialog whose hook
	// holds the decision is the hook's to report.
	if !open || (turn.Status == TurnBlocked && !renewing) || (!renewing && w.hookHoldsPrompt(id, activity.Prompt)) {
		return turn, hasTurn
	}
	summary := activity.Summary
	if prompt := activity.Prompt; prompt != nil && prompt.Title != "" && prompt.Title != summary {
		summary = summary + ": " + prompt.Title
	}
	details := PromptEventDetails(id, activity.Prompt)
	details["pane"] = strings.TrimSpace(id)
	raw, err := json.Marshal(details)
	if err != nil {
		return turn, hasTurn
	}
	if _, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		TurnID: turnID, Status: "blocked", Phase: "working", Attention: "user_input",
		Summary: summary, EventKind: "needs_judgment", DetailsJSON: string(raw), LeaseSeconds: promptGateLeaseSeconds,
		ProgressEventID: fmt.Sprintf("pane-gate:%s:%s:%d", turnID, promptID, time.Now().UnixNano()),
	}); err != nil {
		return turn, hasTurn
	}
	w.mu.Lock()
	w.gates[id] = promptGate{turnID: turnID, promptID: promptID, filedAt: time.Now(), permission: activity.Prompt != nil && activity.Prompt.Kind == classifier.PromptPermission}
	w.mu.Unlock()
	refresh()
	return turn, hasTurn
}

// PromptEventDetails is what Brain reads in a Worker's needs_input for a
// blocking prompt, whichever path caught it: the pane gate or the permission
// hook. Both name the same event, carry the same structured prompt, and end in
// the one command that answers it.
func PromptEventDetails(sessionID string, prompt *classifier.Prompt) map[string]any {
	details := map[string]any{"event": "prompt_blocked"}
	if prompt == nil {
		return details
	}
	details["prompt"] = prompt
	details["answer_with"] = answerWith(sessionID, prompt)
	if prompt.Kind == classifier.PromptPermission {
		details["policy"] = permissionPolicy
	}
	return details
}

const permissionPolicy = "Choose Allow only for what the Work's brief already authorizes, and quote that sentence as -basis. Use -escalate for anything the brief does not cover (other people's data, money, accounts or credentials, outside messages, irreversible deletion): the user then gets a card. Choose Deny to refuse it, with -basis saying why. If you do not answer, the user is asked."

// answerWith is the command Brain answers a prompt with.
func answerWith(sessionID string, prompt *classifier.Prompt) string {
	switch prompt.Kind {
	case classifier.PromptChoice:
		return fmt.Sprintf(`mewla worker answer -id %s -prompt %s -answers '<one {"selected":[<option number>],"other":"<text>"} per question, as a JSON array>'`, sessionID, prompt.ID)
	case classifier.PromptPermission:
		return fmt.Sprintf(`mewla worker answer -id %s -prompt %s -choice <option number> -basis "<the sentence of the brief that authorizes it>"  (or -escalate -basis "<why>" to hand it to the user)`, sessionID, prompt.ID)
	default:
		return fmt.Sprintf("mewla worker answer -id %s -prompt %s -choice <option number>", sessionID, prompt.ID)
	}
}

// SetApprovalHold installs how the gate learns that a permission hook holds
// the decision of a Session's dialog, so one dialog is reported once.
func (w *Watcher) SetApprovalHold(holds func(sessionID string) bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.approvalHolds = holds
	w.mu.Unlock()
}

func (w *Watcher) hookHoldsPrompt(sessionID string, prompt *classifier.Prompt) bool {
	if prompt == nil || prompt.Kind != classifier.PromptPermission {
		return false
	}
	w.mu.RLock()
	holds := w.approvalHolds
	w.mu.RUnlock()
	return holds != nil && holds(sessionID)
}

// PanePromptFiled reports whether the pane gate has already told Brain about
// the permission dialog open in a Session, so the hook that holds its decision
// does not tell Brain again.
func (w *Watcher) PanePromptFiled(sessionID string) bool {
	w.mu.RLock()
	gate, filed := w.gates[sessionID]
	w.mu.RUnlock()
	return filed && gate.permission
}

// AnswerPrompt presses the option Number of the numbered menu open in a
// Session's pane, after proving the menu is still the prompt the answer was
// composed for. A choice prompt (AskUserQuestion) is answered through
// AnswerClaudeChoice instead.
func (w *Watcher) AnswerPrompt(sessionID, promptID string, number int) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.TrimSpace(promptID) == "" {
		return refusePrompt("name the Session and the prompt")
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return refusePrompt("target provider could not be proven; no key was sent")
	}
	pane := &tmuxClaudeChoicePane{watcher: w, sessionID: sessionID, identity: identity}
	return w.sessionInputOwner().serialized(sessionID, func() error {
		prompt, err := w.currentPrompt(pane, sessionID)
		if err != nil {
			return err
		}
		if prompt == nil || prompt.ID != promptID {
			return fmt.Errorf("%w: %w", ErrPromptGone, ErrClaudeChoiceRefused)
		}
		if prompt.Kind == classifier.PromptChoice {
			return refusePrompt("a question is answered with its answers, not one option")
		}
		valid := false
		for _, option := range prompt.Options {
			valid = valid || option.Number == number
		}
		if !valid || number < 1 || number > 9 {
			return refusePrompt("the prompt has no option %d", number)
		}
		if err := pane.sendKey(fmt.Sprint(number)); err != nil {
			return err
		}
		deadline := time.Now().Add(claudeChoiceStepTimeout)
		for {
			time.Sleep(claudeChoicePollInterval)
			after, err := w.currentPrompt(pane, sessionID)
			if err != nil {
				return err
			}
			if after == nil || after.ID != promptID {
				return nil
			}
			if !time.Now().Before(deadline) {
				return refusePrompt("the prompt did not take option %d", number)
			}
		}
	})
}

func refusePrompt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClaudeChoiceRefused, fmt.Sprintf(format, args...))
}

// CurrentPrompt reads the prompt open in a Session's pane right now.
func (w *Watcher) CurrentPrompt(sessionID string) (*classifier.Prompt, error) {
	sessionID = strings.TrimSpace(sessionID)
	identity, known := w.targetForSession(sessionID)
	if !known {
		return nil, refusePrompt("target provider could not be proven")
	}
	return w.currentPrompt(&tmuxClaudeChoicePane{watcher: w, sessionID: sessionID, identity: identity}, sessionID)
}

func (w *Watcher) currentPrompt(pane claudeChoicePane, sessionID string) (*classifier.Prompt, error) {
	content, err := pane.capture()
	if err != nil {
		return nil, err
	}
	worker := w.GetWorker(sessionID)
	if worker == nil {
		return nil, refusePrompt("unknown Session")
	}
	signal := withChoicePrompt(w.activitySignal(*worker, content, 0, nil), content)
	if signal.State != classifier.StateBlocked {
		return nil, nil
	}
	return signal.Prompt, nil
}
