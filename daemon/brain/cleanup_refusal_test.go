package brain

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A completed delegated Session whose pane is now something Mewla does not own
// (a shell left after the agent exited) is refused, never killed, and the
// refusal is reported as one that retrying cannot change.
type refusingCleanupWatcher struct {
	*fakeWatcher
	refuse map[string]error
	calls  map[string]int
}

func (w *refusingCleanupWatcher) KillCompletedSession(sessionID, turnID string) error {
	w.calls[sessionID]++
	if err := w.refuse[sessionID]; err != nil {
		return err
	}
	return w.fakeWatcher.KillCompletedSession(sessionID, turnID)
}

func completeWorkOn(t *testing.T, store *Store, service *Service, fw *refusingCleanupWatcher, sessionID string) {
	t.Helper()
	item, err := store.CreateWork(Work{Title: sessionID, Objective: "cleanup refusal"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SubmitDelegatedWorkInput(sessionID, "task", item.ID, sessionID+"-turn", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: sessionID, TurnID: sessionID + "-turn", Class: watcher.EvidenceControl, Kind: "done", SourceID: "done", At: time.Now()}); err != nil || !result.Changed {
		t.Fatalf("report: %+v %v", result, err)
	}
	done := WorkDone
	if _, err := store.UpdateWork(item.ID, WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCleanupRefusalIsReportedAsFinalAndNeverKills(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &refusingCleanupWatcher{
		fakeWatcher: &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Worker{
			"%6": {ID: "%6", Delegated: true, State: classifier.StateDone},
		}},
		refuse: map[string]error{"%6": fmt.Errorf("Session %%6 is %w", watcher.ErrNotOwnedDelegatedWorker)},
		calls:  map[string]int{},
	}
	service := NewService(store, fw, nil)
	completeWorkOn(t, store, service, fw, "%6")

	complete, err := service.ReconcileSignalSystemStartup([]*classifier.Worker{{ID: "%6", State: classifier.StateDone}}, 8)
	if err == nil || !errors.Is(err, watcher.ErrNotOwnedDelegatedWorker) {
		t.Fatalf("refusal must stay visible, got %v", err)
	}
	if !complete || !OnlyCleanupRefusals(err) {
		t.Fatalf("complete=%v onlyRefusals=%v for %v", complete, OnlyCleanupRefusals(err), err)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("a Session Mewla does not own was killed: %v", fw.killed)
	}
}

func TestOnlyCleanupRefusalsIgnoresMixedAndUnrelatedFailures(t *testing.T) {
	refusal := &cleanupRefusal{sessionID: "a", err: watcher.ErrUnownedTmuxTarget}
	transient := errors.New("tmux server unreachable")
	if !OnlyCleanupRefusals(errors.Join(refusal, errors.Join(refusal))) {
		t.Fatal("nested refusals are final")
	}
	if OnlyCleanupRefusals(errors.Join(refusal, transient)) {
		t.Fatal("a transient failure next to a refusal must still be retried")
	}
	if OnlyCleanupRefusals(transient) || OnlyCleanupRefusals(watcher.ErrUnownedTmuxTarget) {
		t.Fatal("only errors the cleanup marked as refused are final")
	}
	if !OnlyCleanupRefusals(nil) {
		t.Fatal("no error is trivially final")
	}
}
