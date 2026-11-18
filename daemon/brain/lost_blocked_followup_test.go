package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// FreerideCiToAws 11-19 00:09: a Worker's signal turn asked the owner
// (blocked) and was then lost without a report. The Work released its
// Attempt; the ledger reads the Session's turn as unknown, but the turn's own
// row is still open, and a provider fact applied to it answers it unfinished.
// No Attempt is left to steer, so a follow-up is a Fresh admission, and the
// lifecycle takes it.
func TestFollowUpAfterALostBlockedSignalTurnIsFresh(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "lost blocked", "worker")
	sessionID, lostTurn := "worker", "worker-turn-1"
	if _, created, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: lostTurn, Receipt: lostTurn,
		PayloadSHA256: pendingSubmissionDigest("ask"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	for i, kind := range []string{"running", "blocked"} {
		if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
			SessionID: sessionID, TurnID: lostTurn, Class: watcher.EvidenceControl, Kind: kind,
			SourceID: "control\x00" + lostTurn + "\x00" + kind, At: base.Add(time.Duration(i+1) * time.Second),
		}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || state.Attempt == nil {
		t.Fatalf("no Attempt before the loss: %+v %v", state, err)
	}
	if _, err := store.FSM().ReportTurnLost(state.ID, lifecycle.AttemptIdentity{
		SessionID: state.Attempt.SessionID, TurnToken: state.Attempt.TurnToken, Fence: state.Attempt.Generation,
	}, "stale"); err != nil {
		t.Fatal(err)
	}

	current, found, err := store.Turn(sessionID)
	if err != nil || !found || current.TurnID != lostTurn || !watcher.TurnTerminal(current.Status) {
		t.Fatalf("the lost turn does not read as over: %+v found=%v err=%v", current, found, err)
	}
	row, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: lostTurn, Class: watcher.EvidenceProvider, Kind: "done",
		SourceID: "provider\x00settled", At: base.Add(time.Minute), Summary: "Delegated provider activity completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if watcher.TurnTerminal(row.Status) {
		t.Fatalf("the provider fact settled the lost row (%s): the split this guards against is gone", row.Status)
	}

	steer := watcher.InputAdmission{
		WorkID: item.ID, SessionID: sessionID, ProposedTurnID: "worker-turn-2", Receipt: "worker-turn-2",
		PayloadSHA256: pendingSubmissionDigest("follow-up"), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: base.Add(2 * time.Minute), Mode: watcher.InputAdmissionConditionalSteer,
		ExistingTurnID: lostTurn, BaselineActivityID: "activity-last", SignalProtocol: true,
	}
	if _, _, err := store.PrepareInputAdmission(steer); !errors.Is(err, lifecycle.ErrStaleInput) {
		t.Fatalf("steering the lost turn: %v, want the stale-fence refusal Brain saw", err)
	}
	fresh := steer
	fresh.Mode, fresh.ExistingTurnID, fresh.BaselineActivityID = watcher.InputAdmissionFresh, "", ""
	if _, created, err := store.PrepareInputAdmission(fresh); err != nil || !created {
		t.Fatalf("the follow-up was not admitted fresh: created=%v err=%v", created, err)
	}
}
