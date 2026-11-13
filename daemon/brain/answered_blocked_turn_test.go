package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// The owner's shot 11 (2026-11-13): a delegated Worker reported
// --attention user_input, Brain answered with `worker send`, the Worker went
// on working, and its turn stayed blocked (and "Needs you") until the Worker's
// next check-in with the new turn id, 1 to 8 minutes later, or for good if it
// never checks in. Brain's answer typed into the Worker's terminal is the
// answer: from then the turn runs. A send to a Worker that is not waiting on a
// question changes nothing.
func TestAnswerTypedIntoABlockedWorkerEndsItsQuestion(t *testing.T) {
	for _, mode := range []watcher.InputAdmissionMode{watcher.InputAdmissionConditionalSteer, watcher.InputAdmissionFresh} {
		t.Run(string(mode), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			base := time.Now().UTC()
			store.now = func() time.Time { return base }
			item := createSignalTestWork(t, store, "plaza fix", "worker")
			sessionID, oldTurn, newTurn := "worker", "worker-turn-1", "worker-turn-2"
			if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
				WorkID: item.ID, SessionID: sessionID, ProposedTurnID: oldTurn, Receipt: oldTurn,
				PayloadSHA256: pendingSubmissionDigest("brief"), ProcessIdentity: "proc", PaneGeneration: "pane",
				AcceptedAt: base, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
			}); err != nil || !created {
				t.Fatalf("prepare brief created=%v err=%v", created, err)
			}
			if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
				SessionID: sessionID, TurnID: oldTurn, Class: watcher.EvidenceControl, Kind: "attention",
				SourceID: "control\x00ask", Summary: "Plaza fix done. Should I also purge the cache?", At: base.Add(time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			if blocked, _, _ := store.Turn(sessionID); blocked.Status != watcher.TurnBlocked || blocked.Attention != "user_input" {
				t.Fatalf("the report did not block the turn: %+v", blocked)
			}

			if mode == watcher.InputAdmissionFresh {
				// Brain handling the Worker's report: its answer is the Review's follow-up.
				claimAndDeliverTestReview(t, store, "brain-host:@1")
			}
			answer := watcher.InputAdmission{
				WorkID: item.ID, SessionID: sessionID, ProposedTurnID: newTurn, Receipt: newTurn,
				PayloadSHA256: pendingSubmissionDigest("answer"), ProcessIdentity: "proc", PaneGeneration: "pane",
				AcceptedAt: base.Add(time.Minute), Mode: mode, ExistingTurnID: oldTurn, SignalProtocol: true,
			}
			if mode == watcher.InputAdmissionConditionalSteer {
				answer.BaselineActivityID = "observed-activity"
			}
			if _, created, err := store.PrepareInputAdmission(answer); err != nil || !created {
				t.Fatalf("prepare answer created=%v err=%v", created, err)
			}
			// Prepared is not yet typed: the question still stands.
			if still, _, _ := store.Turn(sessionID); still.Status != watcher.TurnBlocked {
				t.Fatalf("a prepared answer cleared the question before it was typed: %+v", still)
			}
			if err := store.MarkInputAdmissionTransportStarted(sessionID, newTurn); err != nil {
				t.Fatal(err)
			}
			answered, _, err := store.Turn(sessionID)
			if err != nil || answered.Status != watcher.TurnRunning || answered.Attention != "" {
				t.Fatalf("the typed answer left the question open: %+v err=%v", answered, err)
			}
			work, err := store.Work(item.ID)
			if err != nil || work.Status == WorkNeedsInput {
				t.Fatalf("the Work still waits on input: %+v err=%v", work.Status, err)
			}
			// The Worker's first check-in with the new turn id still promotes it.
			if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
				SessionID: sessionID, TurnID: newTurn, Class: watcher.EvidenceControl, Kind: "running",
				SourceID: "control\x00go-on", At: base.Add(2 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
			if current, _, _ := store.Turn(sessionID); current.TurnID != newTurn || current.Status != watcher.TurnRunning {
				t.Fatalf("the next check-in did not take over: %+v", current)
			}
		})
	}
}
