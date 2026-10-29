package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A Worker reports done and stops while a background sub-agent keeps running.
// The sub-agent can still ask for something, and Brain must hear it. The
// settled turn is immutable, so the question cannot be filed on it; it opens a
// turn of its own for the same Work.
func TestAQuestionAfterTheTurnSettledOpensItsOwnBlockedTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "late ask", Objective: "run a sub-agent"})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID, firstTurn = "worker:@late", "turn:first"
	acceptedAt := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	first := watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: firstTurn, Receipt: firstTurn,
		PayloadSHA256: pendingSubmissionDigest("brief"), ProcessIdentity: "process-1",
		PaneGeneration: "pane-1", AcceptedAt: acceptedAt, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}
	if _, created, err := store.PrepareInputAdmission(first); err != nil || !created {
		t.Fatalf("prepare the Worker's prompt: created=%v err=%v", created, err)
	}
	fact := func(turnID, kind, source string, at time.Time) watcher.TurnFact {
		return watcher.TurnFact{
			SessionID: sessionID, TurnID: turnID, Class: watcher.EvidenceControl,
			Kind: kind, SourceID: source, Summary: kind, At: at, LeaseSeconds: 300,
		}
	}
	if result, err := store.ApplyDelegatedTurnProgress(fact(firstTurn, "running", "running", acceptedAt.Add(time.Second))); err != nil || !result.Matched {
		t.Fatalf("the Worker's first report: %+v %v", result, err)
	}
	if result, err := store.ApplyDelegatedTurnProgress(fact(firstTurn, "done", "done", acceptedAt.Add(time.Minute))); err != nil || !result.Changed || result.Turn.Status != watcher.TurnDone {
		t.Fatalf("the Worker's own report did not settle its turn: %+v %v", result, err)
	}

	// The Worker is no Work's Attempt any more, yet the Work it belongs to is known.
	if workID, ok := store.WorkOfSession(sessionID); !ok || workID != item.ID {
		t.Fatalf("WorkOfSession = %q %v, want %q", workID, ok, item.ID)
	}
	if _, ok := store.WorkOfSession("worker:@other"); ok {
		t.Fatal("an unrelated Session was given a Work")
	}

	// The settled turn takes no question: nothing can be filed on it.
	if result, err := store.ApplyDelegatedTurnProgress(fact(firstTurn, "attention", "late-ask-on-settled", acceptedAt.Add(2*time.Minute))); err != nil || result.Matched || result.Changed {
		t.Fatalf("a settled turn took a question: %+v %v", result, err)
	}

	// On a turn of its own it blocks, and Brain's review of it is raised.
	const second = "turn:late-ask"
	pending, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: second, Receipt: second,
		PayloadSHA256: pendingSubmissionDigest("prompt-turn"), ProcessIdentity: "process-1",
		PaneGeneration: "pane-1", AcceptedAt: acceptedAt.Add(3 * time.Minute),
		Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	})
	if err != nil || !created || pending.State != watcher.InputAdmissionPending {
		t.Fatalf("prepare the question's turn: %+v created=%v err=%v", pending, created, err)
	}
	result, err := store.ApplyDelegatedTurnProgress(fact(second, "attention", "late-ask", acceptedAt.Add(4*time.Minute)))
	if err != nil || !result.Matched || !result.Changed || result.Turn.Status != watcher.TurnBlocked || result.Turn.Attention != "user_input" {
		t.Fatalf("the question did not block its turn: %+v %v", result, err)
	}
	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != second || current.Status != watcher.TurnBlocked {
		t.Fatalf("current turn: %+v found=%v err=%v", current, found, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var raised bool
	for _, event := range events {
		if event.Kind == "session.needs_input" && event.Actionable {
			raised = true
		}
	}
	if !raised {
		t.Fatalf("Brain was not told: %+v", events)
	}

	// The question's review took the place of the review of the Worker's
	// reported result. Answering settles the question's turn as done again, so
	// Brain is handed that result to review once more.
	if result, err := store.ApplyDelegatedTurnProgress(fact(second, "done", "late-ask-answered", acceptedAt.Add(5*time.Minute))); err != nil || !result.Changed || result.Turn.Status != watcher.TurnDone {
		t.Fatalf("settling the question's turn: %+v %v", result, err)
	}
	reviewed, err := store.Work(item.ID)
	if err != nil || reviewed.Status != WorkWaiting || reviewed.NextAction != "Review the delegated Session result." {
		t.Fatalf("Brain is not handed the result again: %+v %v", reviewed, err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Review == nil || state.Review.Reason != "turn_done" || state.Review.Ref != second {
		t.Fatalf("review after the answer: %+v %v", state.Review, err)
	}
}
