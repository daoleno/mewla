package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// TestSteerWhosePredecessorEndedFirstIsAdmittedOnItsOwnSignal reproduces
// ZeroGAccountsPlans 11-15 22:43: Brain steered a prompt into a running turn,
// the Worker reported that turn done one second later, and the steered text
// still reached the agent. Every report the Worker then made with the new
// prompt's exact --turn-id was refused with "stale fence or turn token" for
// minutes, so its "needs the owner" never reached Brain. The exact signal is
// proof the prompt landed: with the predecessor over, it is a follow-up.
func TestSteerWhosePredecessorEndedFirstIsAdmittedOnItsOwnSignal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "steer race", "worker")
	sessionID := "worker"
	oldTurn := "worker-turn-1"

	if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: oldTurn, Receipt: oldTurn,
		PayloadSHA256: pendingSubmissionDigest("old"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("prepare active turn created=%v err=%v", created, err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: oldTurn, Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "control\x00" + oldTurn, At: base.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	newTurn := "worker-turn-2"
	if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: newTurn, Receipt: newTurn,
		PayloadSHA256: pendingSubmissionDigest("new"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base.Add(2 * time.Second), Mode: watcher.InputAdmissionConditionalSteer,
		ExistingTurnID: oldTurn, BaselineActivityID: "observed-activity", SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("conditional steer prepare created=%v err=%v", created, err)
	}
	// The predecessor ends before the steered prompt's first signal.
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: oldTurn, Class: watcher.EvidenceControl, Kind: "done",
		SourceID: "control\x00" + oldTurn + "\x00done", At: base.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt != nil {
		t.Fatalf("predecessor done left an attempt: %+v", state.Attempt)
	}

	result, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: sessionID, TurnID: newTurn, Class: watcher.EvidenceControl, Kind: "attention",
		Attention: "user_input", Summary: "GitHub login open on my Screen",
		SourceID: "control\x00" + newTurn + "\x00ask", At: base.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("steered prompt's own signal failed: %v", err)
	}
	if !result.Owned || !result.Matched || result.Rejection != "" {
		t.Fatalf("steered prompt's own signal was not recorded: %+v", result)
	}
	state, _ = store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Attempt == nil || state.Attempt.TurnToken != lifecycle.TurnToken(newTurn) || state.Attempt.FollowUpOf != lifecycle.TurnToken(oldTurn) {
		t.Fatalf("steered prompt did not become the follow-up attempt: %+v", state.Attempt)
	}
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != newTurn || current.Attention != "user_input" {
		t.Fatalf("current turn = %+v found=%v err=%v", current, found, err)
	}
}

// TestSteerWhosePredecessorWasReplacedExplainsTheRefusal: when another prompt
// took over the Work before the steered one signalled, the late report is
// refused with the turn to use, not a bare "stale fence or turn token".
func TestSteerWhosePredecessorWasReplacedExplainsTheRefusal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "steer replaced", "worker")
	sessionID := "worker"
	prepare := func(turn string, at time.Duration, mode watcher.InputAdmissionMode, existing string) {
		t.Helper()
		if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
			WorkID: item.ID, SessionID: sessionID, ProposedTurnID: turn, Receipt: turn,
			PayloadSHA256: pendingSubmissionDigest(turn), ProcessIdentity: "proc", PaneGeneration: "pane",
			AcceptedAt: base.Add(at), Mode: mode, ExistingTurnID: existing,
			BaselineActivityID: map[bool]string{true: "observed-activity"}[existing != ""], SignalProtocol: true,
		}); err != nil || !created {
			t.Fatalf("prepare %s created=%v err=%v", turn, created, err)
		}
	}
	signal := func(turn, kind string, at time.Duration) (watcher.TurnProgressResult, error) {
		return store.ApplyDelegatedTurnProgress(watcher.TurnFact{
			SessionID: sessionID, TurnID: turn, Class: watcher.EvidenceControl, Kind: kind,
			SourceID: "control\x00" + turn + "\x00" + kind, At: base.Add(at),
		})
	}
	prepare("turn-1", 0, watcher.InputAdmissionFresh, "")
	if _, err := signal("turn-1", "running", time.Second); err != nil {
		t.Fatal(err)
	}
	prepare("turn-2", 2*time.Second, watcher.InputAdmissionConditionalSteer, "turn-1")
	if _, err := signal("turn-1", "done", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	// Brain gave up waiting for turn-2's signal and sent a fresh prompt.
	if _, err := store.FSM().MarkAdmissionAmbiguous(lifecycle.WorkID(item.ID), "turn-2", "no signal"); err != nil {
		t.Fatal(err)
	}
	prepare("turn-3", 3500*time.Millisecond, watcher.InputAdmissionFresh, "")
	if result, err := signal("turn-3", "running", 4*time.Second); err != nil || !result.Matched {
		t.Fatalf("turn-3 signal result=%+v err=%v", result, err)
	}
	ask := func() (watcher.TurnProgressResult, error) {
		return store.ApplyDelegatedTurnProgress(watcher.TurnFact{
			SessionID: sessionID, TurnID: "turn-2", Class: watcher.EvidenceControl, Kind: "running",
			Attention: "user_input", Summary: "GitHub login open on my Screen",
			SourceID: "control\x00turn-2\x00ask", At: base.Add(5 * time.Second),
		})
	}
	result, err := ask()
	if err != nil {
		t.Fatalf("a refused report must be a rejection, not an error: %v", err)
	}
	if result.Matched || !strings.Contains(result.Rejection, "turn-3") {
		t.Fatalf("refusal does not name the current turn: %+v", result)
	}
	// The refused ask is not dropped: it is on the Work for Brain and the
	// card, once however often the Worker retries it.
	if _, err := ask(); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var refused []WorkEvent
	for _, event := range events {
		if event.Kind == workerReportRefusedKind {
			refused = append(refused, event)
		}
	}
	if len(refused) != 1 || !strings.Contains(refused[0].Summary, "GitHub login open on my Screen") ||
		!strings.Contains(refused[0].Summary, "turn-3") || refused[0].Attention != "user_input" {
		t.Fatalf("refused ask did not reach the Work once: %+v", refused)
	}
}
