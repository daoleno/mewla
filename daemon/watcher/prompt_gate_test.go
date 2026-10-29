package watcher

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

const subAgentGatePane = "Claude Code\n● Agent(recon)\n────────────\n Bash command · from the general-purpose agent\n │ cd /x && sed -i s/a/b/ y\n │ Compound command contains cd with write operation - manual approval required\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend · ctrl+x ctrl+k twice to stop background agents\n"

// A delegated Worker blocked on a prompt nobody sits at must tell Brain: the
// prompt is filed to its turn once, with its options, and filed again as
// running when it goes away. The sub-agent compound-cd permission prompt of
// 2026-10-29 sat unseen for 30 minutes because the pane gate was only a colour.
func TestPromptGateFilesBlockedWithOptionsAndClears(t *testing.T) {
	w := New(0)
	sessionID := "mewla-worker-gate:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateRunning, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	ledger := newFakeTurnLedger()
	turn := TurnSnapshot{SessionID: sessionID, TurnID: "turn:gate", Status: TurnRunning, SignalProtocol: true}
	ledger.seed(sessionID, turn)
	w.SetTurnLedger(ledger)

	adapter := classifier.NewClaudeActivityAdapter()
	open := adapter.Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})
	if open.Prompt == nil {
		t.Fatalf("fixture has no structured prompt: %+v", open)
	}

	turn = w.syncPromptGate(sessionID, worker, true, open, turn)
	if turn.Status != TurnBlocked || turn.Attention != "user_input" {
		t.Fatalf("turn not blocked: %+v", turn)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].Kind != "attention" || ledger.applied[0].Class != EvidenceControl {
		t.Fatalf("applied: %+v", ledger.applied)
	}
	var details struct {
		Event      string            `json:"event"`
		Prompt     classifier.Prompt `json:"prompt"`
		AnswerWith string            `json:"answer_with"`
	}
	if err := json.Unmarshal([]byte(ledger.applied[0].DetailsJSON), &details); err != nil {
		t.Fatal(err)
	}
	if details.Event != "prompt_blocked" || details.Prompt.ID != open.Prompt.ID || len(details.Prompt.Options) != 2 ||
		!strings.Contains(details.AnswerWith, "worker answer -id "+sessionID+" -prompt "+open.Prompt.ID) {
		t.Fatalf("Brain would not learn the question and options: %+v", details)
	}

	// The same prompt on the next poll is not filed again.
	w.syncPromptGate(sessionID, worker, true, open, turn)
	if len(ledger.applied) != 1 {
		t.Fatalf("the same prompt was filed twice: %+v", ledger.applied)
	}

	// Answered: the pane no longer shows it.
	w.syncPromptGate(sessionID, worker, true, classifier.ActivitySignal{}, turn)
	if len(ledger.applied) != 2 || ledger.applied[1].Kind != "running" || ledger.applied[1].Attention != "none" {
		t.Fatalf("the answered prompt was not cleared: %+v", ledger.applied)
	}
}

func TestPromptGateIgnoresWorkersItDoesNotOwn(t *testing.T) {
	w := New(0)
	ledger := newFakeTurnLedger()
	w.SetTurnLedger(ledger)
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "claude"}, PaneContent: subAgentGatePane})
	for name, tc := range map[string]struct {
		worker classifier.Worker
		turn   TurnSnapshot
	}{
		"not delegated": {classifier.Worker{ID: "a", Command: "claude"}, TurnSnapshot{TurnID: "t", Status: TurnRunning, SignalProtocol: true}},
		"no signal":     {classifier.Worker{ID: "a", Delegated: true, Command: "claude"}, TurnSnapshot{TurnID: "t", Status: TurnRunning}},
		"settled":       {classifier.Worker{ID: "a", Delegated: true, Command: "claude"}, TurnSnapshot{TurnID: "t", Status: TurnDone, SignalProtocol: true}},
	} {
		t.Run(name, func(t *testing.T) {
			w.syncPromptGate("a", tc.worker, true, open, tc.turn)
			if len(ledger.applied) != 0 {
				t.Fatalf("filed a prompt for a turn it does not own: %+v", ledger.applied)
			}
		})
	}
}
