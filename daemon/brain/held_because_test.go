package brain

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/watcher"
)

// #30 keeps a reclaimable Session that is still busy; the reason it is kept
// goes to the collector's ledger once it has held for reclaimWaitReported,
// and is cleared when the wait ends.
func TestAHeldSessionsReasonReachesTheLedger(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := NewStore(filepath.Join(stateRoot, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	service := &Service{store: store, now: func() time.Time { return now }}
	gc.BindSession(stateRoot, "mewla-worker-o-unit.scope", "mh-busy", now)
	busy := fmt.Errorf("%w: a build runs", watcher.ErrSessionNotIdle)

	service.noteReclaimWait("mh-busy", "its Work is closed", busy)
	now = now.Add(5 * time.Minute)
	service.noteReclaimWait("mh-busy", "its Work is closed", busy)
	if held, _ := gc.HeldSessions(stateRoot); len(held) != 0 {
		t.Fatalf("held after 5 min: %+v", held)
	}
	now = now.Add(6 * time.Minute)
	service.noteReclaimWait("mh-busy", "its Work is closed", busy)
	held, err := gc.HeldSessions(stateRoot)
	if err != nil || len(held) != 1 || held[0].HeldBecause != "its Work is closed: a build runs" || !held[0].HeldSince.Equal(now.Add(-11*time.Minute)) {
		t.Fatalf("held %+v err %v", held, err)
	}
	service.forgetReclaimWaits(map[string]bool{})
	if held, _ := gc.HeldSessions(stateRoot); len(held) != 0 {
		t.Fatalf("held after the wait ended: %+v", held)
	}
}
