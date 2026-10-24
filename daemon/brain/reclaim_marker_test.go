package brain

import (
	"fmt"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

func newRefusingService(t *testing.T, dir string, sessions ...string) (*Store, *Service, *refusingCleanupWatcher) {
	t.Helper()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	workers := map[string]*classifier.Worker{}
	for _, id := range sessions {
		workers[id] = &classifier.Worker{ID: id, Delegated: true, State: classifier.StateDone}
	}
	fw := &refusingCleanupWatcher{
		fakeWatcher: &fakeWatcher{turnStore: store, sessions: workers},
		refuse:      map[string]error{},
		calls:       map[string]int{},
	}
	return store, NewService(store, fw, nil), fw
}

// A Session whose teardown finished is not torn down again on the next pass,
// and that survives a restart (reopening the store on the same directory).
func TestFinishedTeardownIsMarkedAndNotReplayed(t *testing.T) {
	dir := t.TempDir()
	store, service, fw := newRefusingService(t, dir, "%1", "%2")
	completeWorkOn(t, store, service, fw, "%1")
	completeWorkOn(t, store, service, fw, "%2")
	if err := service.ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if fw.calls["%1"] != 1 || fw.calls["%2"] != 1 {
		t.Fatalf("first pass must tear each Session down once: %v", fw.calls)
	}
	if turns, err := store.CompletedOwnedTurns(""); err != nil || len(turns) != 0 {
		t.Fatalf("reclaimed Turns are still in the cleanup set: %+v %v", turns, err)
	}
	fw.calls = map[string]int{}
	for pass := 0; pass < 3; pass++ {
		if err := service.ReconcileCompletedSessions(); err != nil {
			t.Fatal(err)
		}
	}
	if len(fw.calls) != 0 {
		t.Fatalf("finished teardown was replayed: %v", fw.calls)
	}

	reopened, reopenedService, reopenedFw := newRefusingService(t, dir, "%1", "%2")
	if err := reopenedService.ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if len(reopenedFw.calls) != 0 {
		t.Fatalf("marker did not survive a restart: %v", reopenedFw.calls)
	}
	_ = reopened
}

// Refused, failed and terminal-error teardowns are never marked: the pane may
// later vanish and its binding and resources still need releasing.
func TestUnfinishedTeardownIsNotMarkedAndRetries(t *testing.T) {
	store, service, fw := newRefusingService(t, t.TempDir(), "%3", "%4", "%5")
	fw.refuse["%3"] = fmt.Errorf("Session %%3 is %w", watcher.ErrNotOwnedDelegatedWorker)
	fw.refuse["%4"] = completedGenerationErr("%4")
	fw.refuse["%5"] = fmt.Errorf("tmux server unreachable")
	for _, id := range []string{"%3", "%4", "%5"} {
		completeWorkOn(t, store, service, fw, id)
	}
	fw.calls = map[string]int{}
	if err := service.ReconcileCompletedSessions(); err == nil {
		t.Fatal("expected the refusals to stay visible")
	}
	for _, id := range []string{"%3", "%4", "%5"} {
		if fw.calls[id] != 1 {
			t.Fatalf("%s attempted %d times in one pass", id, fw.calls[id])
		}
	}
	turns, err := store.CompletedOwnedTurns("")
	if err != nil || len(turns) != 3 {
		t.Fatalf("unfinished Turns must stay in the set: %d %v", len(turns), err)
	}
	fw.refuse = map[string]error{}
	if err := service.ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if turns, _ := store.CompletedOwnedTurns(""); len(turns) != 0 {
		t.Fatalf("a teardown that now succeeds must be marked: %+v", turns)
	}
}

// A pane id reused by another Session ("generation changed") is terminal for
// that record, so startup reconcile completes; a transient error still retries.
func TestGenerationChangedEndsStartupRetryButTransientDoesNot(t *testing.T) {
	store, service, fw := newRefusingService(t, t.TempDir(), "%11", "%12")
	fw.refuse["%11"] = completedGenerationErr("%11")
	completeWorkOn(t, store, service, fw, "%11")
	complete, err := service.ReconcileSignalSystemStartup(nil, 8)
	if err == nil || !OnlyCleanupRefusals(err) || !complete {
		t.Fatalf("generation changed must be final: complete=%v err=%v", complete, err)
	}

	fw.refuse["%12"] = fmt.Errorf("tmux server unreachable")
	completeWorkOn(t, store, service, fw, "%12")
	_, err = service.ReconcileSignalSystemStartup(nil, 8)
	if err == nil || OnlyCleanupRefusals(err) {
		t.Fatalf("a transient error next to a terminal one must still retry: %v", err)
	}
}

func completedGenerationErr(id string) error {
	return fmt.Errorf("completed Session %s %w", id, watcher.ErrCompletedSessionGenerationChanged)
}
