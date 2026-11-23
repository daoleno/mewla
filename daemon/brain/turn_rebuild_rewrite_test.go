package brain

import (
	"testing"
	"time"
)

// A daemon start re-projects every accepted turn; a turn that did not change
// must keep its row, or each start rewrites the whole turn table in one
// transaction (brain.db's WAL grew to 766 MB that way).
func TestRestartLeavesAnUnchangedTurnRowAlone(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return at }
	item, err := store.CreateWork(Work{Title: "steady", Objective: "restart", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	const session, turn = "worker:@1", "turn:steady"
	pending, created, err := store.PrepareInputAdmission(delegatedSubmissionCandidate(item.ID, session, turn, turn, at))
	if err != nil || !created {
		t.Fatalf("prepare=%+v created=%v err=%v", pending, created, err)
	}
	resolveDelegatedSubmission(t, store, pending, "activity", at.Add(time.Millisecond))
	body := func(s *Store) string {
		t.Helper()
		var raw string
		if err := s.db.QueryRow(`SELECT body FROM pres_turn WHERE session_id = ? AND turn_id = ?`, session, turn).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	want := body(store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		reopened, err := NewStore(root) // its clock is the real one, months later
		if err != nil {
			t.Fatal(err)
		}
		if got := body(reopened); got != want {
			t.Fatalf("restart %d rewrote the turn:\n got %s\nwant %s", restart+1, got, want)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
