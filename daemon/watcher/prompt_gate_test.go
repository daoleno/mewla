package watcher

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	turn, _ = w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
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
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 1 {
		t.Fatalf("the same prompt was filed twice: %+v", ledger.applied)
	}

	// Answered: the pane no longer shows it.
	w.syncPromptGate(sessionID, worker, true, classifier.ActivitySignal{}, turn, true, nil)
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
			w.syncPromptGate("a", tc.worker, true, open, tc.turn, true, nil)
			if len(ledger.applied) != 0 {
				t.Fatalf("filed a prompt for a turn it does not own: %+v", ledger.applied)
			}
		})
	}
}

// A Worker can reach a prompt before its first progress report admitted its
// turn (the ledger holds only the prepared prompt). The prompt must still reach
// Brain: the gate's fact promotes the pending turn, as the Worker's own would.
func TestPromptGateReachesBrainBeforeTheTurnIsAdmitted(t *testing.T) {
	w := New(0)
	sessionID := "mewla-worker-early:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateRunning, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	ledger := newFakeTurnLedger()
	w.SetTurnLedger(ledger)
	pending, _, err := ledger.PrepareInputAdmission(InputAdmission{
		WorkID: "work", SessionID: sessionID, ProposedTurnID: "turn:early", SignalProtocol: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})

	turn, has := w.syncPromptGate(sessionID, worker, true, open, TurnSnapshot{}, false, []InputAdmission{pending})
	if !has || turn.TurnID != "turn:early" || turn.Status != TurnBlocked {
		t.Fatalf("the pending turn was not promoted and blocked: %+v has=%v", turn, has)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].TurnID != "turn:early" || ledger.applied[0].Kind != "attention" {
		t.Fatalf("applied: %+v", ledger.applied)
	}
}

// The lifecycle sweep expires a lease whether or not the turn is blocked and
// gives up on it after a grace period; a prompt left unanswered must keep its
// turn alive by renewing the lease while the same prompt stays open.
func TestPromptGateRenewsTheLeaseWhileThePromptStaysOpen(t *testing.T) {
	w := New(0)
	sessionID := "mewla-worker-renew:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateRunning, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	ledger := newFakeTurnLedger()
	turn := TurnSnapshot{SessionID: sessionID, TurnID: "turn:renew", Status: TurnRunning, SignalProtocol: true}
	ledger.seed(sessionID, turn)
	w.SetTurnLedger(ledger)
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})

	turn, _ = w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 1 {
		t.Fatalf("renewed before it was due: %+v", ledger.applied)
	}

	w.mu.Lock()
	gate := w.gates[sessionID]
	gate.filedAt = time.Now().Add(-promptGateRenewEvery - time.Second)
	w.gates[sessionID] = gate
	w.mu.Unlock()
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 2 || ledger.applied[1].Kind != "attention" || ledger.applied[1].LeaseSeconds != promptGateLeaseSeconds {
		t.Fatalf("the lease was not renewed: %+v", ledger.applied)
	}
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 2 {
		t.Fatalf("renewed again immediately: %+v", ledger.applied)
	}
}

// A permission dialog whose hook holds the decision is the hook's to report:
// the pane gate files nothing, so Brain hears of one dialog once. Once the
// pane gate has filed it (it got there first), the hook is told so.
func TestPromptGateLeavesAHookHeldPermissionToTheHook(t *testing.T) {
	w := New(0)
	sessionID := "mewla-worker-hold:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateRunning, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	ledger := newFakeTurnLedger()
	turn := TurnSnapshot{SessionID: sessionID, TurnID: "turn:hold", Status: TurnRunning, SignalProtocol: true}
	ledger.seed(sessionID, turn)
	w.SetTurnLedger(ledger)
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})
	if open.Prompt == nil || open.Prompt.Kind != classifier.PromptPermission {
		t.Fatalf("fixture is not a permission dialog: %+v", open.Prompt)
	}

	held := true
	w.SetApprovalHold(func(string) bool { return held })
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 0 || w.PanePromptFiled(sessionID) {
		t.Fatalf("the pane gate reported a dialog its hook holds: %+v", ledger.applied)
	}

	// Hook not holding it (none installed, or already answered): the gate files.
	held = false
	turn, _ = w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 1 || !w.PanePromptFiled(sessionID) {
		t.Fatalf("the pane gate did not report the dialog: %+v", ledger.applied)
	}
	// The hook then takes it over: no second event, and the clear still happens.
	held = true
	turn, _ = w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 1 {
		t.Fatalf("the dialog was reported twice: %+v", ledger.applied)
	}
	w.syncPromptGate(sessionID, worker, true, classifier.ActivitySignal{}, turn, true, nil)
	if len(ledger.applied) != 2 || ledger.applied[1].Kind != "running" || w.PanePromptFiled(sessionID) {
		t.Fatalf("the answered dialog was not cleared: %+v", ledger.applied)
	}
}

// Whichever path catches a prompt, Brain reads the same event.
func TestPromptEventDetailsAreTheOneShape(t *testing.T) {
	prompt := &classifier.Prompt{ID: "abc", Kind: classifier.PromptPermission, Options: []classifier.PromptOption{{Number: 1, Label: "Yes"}, {Number: 2, Label: "No"}}}
	details := PromptEventDetails("%7", prompt)
	answer, _ := details["answer_with"].(string)
	if details["event"] != "prompt_blocked" || details["prompt"] != prompt ||
		!strings.Contains(answer, "mewla worker answer -id %7 -prompt abc -choice") || !strings.Contains(answer, "-basis") || !strings.Contains(answer, "-escalate") {
		t.Fatalf("details = %+v", details)
	}
	if plain := PromptEventDetails("%7", &classifier.Prompt{ID: "p", Kind: classifier.PromptPlan}); strings.Contains(plain["answer_with"].(string), "-basis") {
		t.Fatalf("a plan prompt asks for a basis: %+v", plain)
	}
}

// A Worker whose turn settled (it reported done and stopped while a background
// sub-agent keeps running) is asked a question by that sub-agent. The settled
// turn is immutable, so the question gets a turn of its own: a prepared signal
// admission for the Work, bound to the live process and pane, that no input was
// sent for.
func TestOpenPromptTurnPreparesASignalTurnForTheWork(t *testing.T) {
	w := New(0)
	sessionID := "mewla-worker-late:@1"
	identity := testSessionInputIdentity("claude")
	w.workers[sessionID] = &classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateDone, Command: "claude"}
	w.workerOrder = append(w.workerOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(identity)
	input := newFakeSessionInputIO()
	input.paneValue.generation = "pane-now"
	w.sessionInput = newSessionInputOwner(input)
	ledger := newFakeTurnLedger()
	w.SetTurnLedger(ledger)

	turnID, err := w.OpenPromptTurn(sessionID, "work-1")
	if err != nil || !strings.HasPrefix(turnID, "turn:") {
		t.Fatalf("OpenPromptTurn = %q, %v", turnID, err)
	}
	pending, err := ledger.PendingInputAdmissions(sessionID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending admissions: %+v %v", pending, err)
	}
	got := pending[0]
	if got.ProposedTurnID != turnID || got.WorkID != "work-1" || !got.SignalProtocol || got.ClaimToken != "" || got.Purpose != "" ||
		got.ProcessIdentity != delegatedTurnIdentity(identity) || got.PaneGeneration != "pane-now" {
		t.Fatalf("prepared admission: %+v", got)
	}
	if len(input.queues) != 0 {
		t.Fatalf("a turn opened for a prompt sent input to the Worker: %+v", input.queues)
	}

	// An unproven target gets no turn.
	w.targetProcessResolver = func(string) (targetProcessIdentity, bool) { return targetProcessIdentity{}, false }
	if _, err := w.OpenPromptTurn(sessionID, "work-1"); err == nil {
		t.Fatal("a turn was opened for a target that could not be proven")
	}
}

type workLedger struct {
	*fakeTurnLedger
	active map[string]string
}

func (l workLedger) ActiveWorkOfSession(sessionID string) (string, bool) {
	workID, ok := l.active[sessionID]
	return workID, ok
}

// A Worker reported done and stopped; a background result wakes it into a
// permission dialog. The settled turn takes nothing, so the dialog is filed on a
// turn opened for it, and answering it settles that turn again.
func TestPromptGateOpensATurnForADialogAfterTheWorkersTurnSettled(t *testing.T) {
	window := latePromptSettle
	latePromptSettle = 0
	t.Cleanup(func() { latePromptSettle = window })
	w := New(0)
	sessionID := "mewla-worker-woken:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateDone, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(testSessionInputIdentity("claude"))
	w.sessionInput = newSessionInputOwner(newFakeSessionInputIO())
	ledger := workLedger{fakeTurnLedger: newFakeTurnLedger(), active: map[string]string{sessionID: "work-1"}}
	settled := TurnSnapshot{SessionID: sessionID, TurnID: "turn:settled", Status: TurnDone, SignalProtocol: true}
	ledger.seed(sessionID, settled)
	w.SetTurnLedger(ledger)
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})
	if open.Prompt == nil {
		t.Fatalf("fixture has no structured prompt: %+v", open)
	}

	turn, _ := w.syncPromptGate(sessionID, worker, true, open, settled, true, nil)
	if turn.Status != TurnBlocked || turn.TurnID == "turn:settled" {
		t.Fatalf("the dialog was not filed on a turn of its own: %+v", turn)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].TurnID != turn.TurnID || ledger.applied[0].Kind != "attention" {
		t.Fatalf("applied: %+v", ledger.applied)
	}
	prepared, found, _ := ledger.InputAdmission(sessionID, turn.TurnID)
	if !found || prepared.WorkID != "work-1" || !prepared.SignalProtocol {
		t.Fatalf("the opened turn belongs to the wrong Work: %+v found=%v", prepared, found)
	}

	// The same dialog on the next poll opens nothing more.
	w.syncPromptGate(sessionID, worker, true, open, turn, true, nil)
	if len(ledger.applied) != 1 {
		t.Fatalf("the same dialog was filed twice: %+v", ledger.applied)
	}

	// Answered: the opened turn settles as done, not running, so Brain reviews
	// the result the Worker had reported.
	w.syncPromptGate(sessionID, worker, true, classifier.ActivitySignal{}, turn, true, nil)
	if len(ledger.applied) != 2 || ledger.applied[1].TurnID != turn.TurnID || ledger.applied[1].Kind != "done" {
		t.Fatalf("the answered dialog did not settle its turn: %+v", ledger.applied)
	}
}

func TestPromptGateOpensNoTurnWhenItShouldNot(t *testing.T) {
	window := latePromptSettle
	t.Cleanup(func() { latePromptSettle = window })
	setup := func(active map[string]string) (*Watcher, classifier.Worker, workLedger, classifier.ActivitySignal, TurnSnapshot) {
		w := New(0)
		sessionID := "mewla-worker-quiet:@1"
		worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateDone, Command: "claude"}
		w.workers[sessionID] = &worker
		w.workerOrder = append(w.workerOrder, sessionID)
		w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
		w.targetProcessResolver = fixedSessionInputResolver(testSessionInputIdentity("claude"))
		w.sessionInput = newSessionInputOwner(newFakeSessionInputIO())
		ledger := workLedger{fakeTurnLedger: newFakeTurnLedger(), active: active}
		settled := TurnSnapshot{SessionID: sessionID, TurnID: "turn:settled", Status: TurnDone, SignalProtocol: true}
		ledger.seed(sessionID, settled)
		w.SetTurnLedger(ledger)
		open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})
		return w, worker, ledger, open, settled
	}
	// A dialog that has only just appeared (or a hook has just had answered).
	latePromptSettle = time.Hour
	w, worker, ledger, open, settled := setup(map[string]string{"mewla-worker-quiet:@1": "work-1"})
	w.syncPromptGate(worker.ID, worker, true, open, settled, true, nil)
	if pending, _ := ledger.PendingInputAdmissions(worker.ID); len(pending) != 0 || len(ledger.applied) != 0 {
		t.Fatalf("a dialog that had not stood a moment opened a turn: %+v %+v", pending, ledger.applied)
	}
	// A Work that is done or unknown to the Session.
	latePromptSettle = 0
	w, worker, ledger, open, settled = setup(map[string]string{})
	w.syncPromptGate(worker.ID, worker, true, open, settled, true, nil)
	if pending, _ := ledger.PendingInputAdmissions(worker.ID); len(pending) != 0 || len(ledger.applied) != 0 {
		t.Fatalf("a turn was opened with no Work to raise it on: %+v %+v", pending, ledger.applied)
	}
	// A dialog a permission hook holds is the hook's to report.
	w, worker, ledger, open, settled = setup(map[string]string{"mewla-worker-quiet:@1": "work-1"})
	w.SetApprovalHold(func(string) bool { return true })
	w.syncPromptGate(worker.ID, worker, true, open, settled, true, nil)
	if pending, _ := ledger.PendingInputAdmissions(worker.ID); len(pending) != 0 || len(ledger.applied) != 0 {
		t.Fatalf("the gate opened a turn for a dialog its hook holds: %+v %+v", pending, ledger.applied)
	}
}

// The settle window is measured across polls: the dialog is noted on the first
// poll that sees it and opens a turn on the first poll after the window.
func TestPromptGateOpensTheTurnOnceTheDialogHasStoodForTheWindow(t *testing.T) {
	window := latePromptSettle
	latePromptSettle = 40 * time.Millisecond
	t.Cleanup(func() { latePromptSettle = window })
	w := New(0)
	sessionID := "mewla-worker-stands:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateDone, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(testSessionInputIdentity("claude"))
	w.sessionInput = newSessionInputOwner(newFakeSessionInputIO())
	ledger := workLedger{fakeTurnLedger: newFakeTurnLedger(), active: map[string]string{sessionID: "work-1"}}
	settled := TurnSnapshot{SessionID: sessionID, TurnID: "turn:settled", Status: TurnDone, SignalProtocol: true}
	ledger.seed(sessionID, settled)
	w.SetTurnLedger(ledger)
	open := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: subAgentGatePane})

	w.syncPromptGate(sessionID, worker, true, open, settled, true, nil)
	if len(ledger.applied) != 0 {
		t.Fatalf("a dialog that had not stood the window opened a turn: %+v", ledger.applied)
	}
	// Polled again inside the window: still the same dialog, still waiting.
	w.syncPromptGate(sessionID, worker, true, open, settled, true, nil)
	if len(ledger.applied) != 0 {
		t.Fatalf("a second poll inside the window opened a turn: %+v", ledger.applied)
	}
	time.Sleep(60 * time.Millisecond)
	w.syncPromptGate(sessionID, worker, true, open, settled, true, nil)
	if len(ledger.applied) != 1 || ledger.applied[0].Kind != "attention" {
		t.Fatalf("the dialog that stood the window was not filed: %+v", ledger.applied)
	}

	// A dialog that went away starts the window over.
	w2 := w
	w2.forgetLatePrompt(sessionID)
	if _, seen := w2.latePrompts[sessionID]; seen {
		t.Fatal("the window survived forgetting the dialog")
	}
}
