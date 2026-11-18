package brain

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A loss says why, and proving the Turn's own process again lifts only the
// control gate: the Turn's outcome, the Work and its lifecycle stay as the
// loss left them.
func TestOwnershipReproofLiftsOnlyTheControlGate(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "reproof", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "reproof-turn", AcceptedAt: base,
	})

	if snapshot, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "worker", TurnID: "reproof-turn", Class: watcher.EvidenceLiveness,
		Kind: watcher.TurnFactOwnershipReproved, SourceID: "liveness\x00p\x00ownership-reproved\x00early", At: base,
	}); err != nil || changed || snapshot.ControlState != watcher.TurnControlOwned {
		t.Fatalf("reproof of an ungated Turn: %+v changed=%v err=%v", snapshot, changed, err)
	}

	reason := watcher.OwnershipLostSummary + ": the agent process now in the Session (claude, pid 50, started 2026-11-18T14:06:02Z) is not the process this turn was given to"
	lost, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "worker", TurnID: "reproof-turn", Class: watcher.EvidenceLiveness,
		Kind: "ownership_lost", SessionReplaced: true, SourceID: "liveness\x00p\x00ownership-lost\x00q",
		At: base.Add(time.Second), Summary: reason,
	})
	if err != nil || !changed || lost.ControlState != watcher.TurnControlOwnershipLost || lost.Status != watcher.TurnUnknown {
		t.Fatalf("loss: %+v changed=%v err=%v", lost, changed, err)
	}
	if !strings.Contains(lost.Summary, "pid 50") {
		t.Fatalf("loss summary does not say why: %q", lost.Summary)
	}
	before, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}

	reproved, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "worker", TurnID: "reproof-turn", Class: watcher.EvidenceLiveness,
		Kind: watcher.TurnFactOwnershipReproved, SourceID: "liveness\x00p\x00ownership-reproved\x00p",
		At: base.Add(2 * time.Second), Summary: "proven again",
	})
	if err != nil || !changed || reproved.ControlState != watcher.TurnControlOwned {
		t.Fatalf("reproof: %+v changed=%v err=%v", reproved, changed, err)
	}
	if reproved.Status != lost.Status || !reflect.DeepEqual(reproved.SettledAt, lost.SettledAt) {
		t.Fatalf("reproof moved the outcome: %+v, was %+v", reproved, lost)
	}
	after, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || !reflect.DeepEqual(after.Attempt, before.Attempt) {
		t.Fatalf("reproof moved the Work: %+v, was %+v", after, before)
	}
}

// The watcher learns that Mewla reclaimed a Session from the Turn it reads.
func TestReclaimedMarkReachesTheWatchersTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base }
	item := createSignalTestWork(t, store, "reclaimed", "worker")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: "worker", TurnID: "reclaimed-turn", AcceptedAt: base,
	})
	turn, found, err := store.Turn("worker")
	if err != nil || !found || turn.Reclaimed {
		t.Fatalf("before: %+v found=%v err=%v", turn, found, err)
	}
	if err := store.MarkTurnsReclaimed([]watcher.TurnSnapshot{turn}); err != nil {
		t.Fatal(err)
	}
	if turn, found, err = store.Turn("worker"); err != nil || !found || !turn.Reclaimed {
		t.Fatalf("after: %+v found=%v err=%v", turn, found, err)
	}
}
