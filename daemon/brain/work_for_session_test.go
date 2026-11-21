package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

func TestWorkForSessionPrefersLiveWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	liveItem, err := store.CreateWork(Work{
		Title: "Live", Objective: "Live work", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	bootstrapAdmittedTurnFixture(t, store, liveItem.ID, watcher.AdmittedTurn{
		SessionID: "sess-1", TurnID: "sess-1:turn:1", AcceptedAt: acceptedAt,
	})
	service := newTestService(t, store, &fakeWatcher{}, nil)
	workItem, found, err := service.WorkForSession("sess-1")
	if err != nil || !found || workItem.ID != liveItem.ID || workItem.AttemptSessionID != "sess-1" {
		t.Fatalf("live work=%+v found=%v err=%v", workItem, found, err)
	}
	if workItem.Status != WorkRunning {
		t.Fatalf("live work status=%s", workItem.Status)
	}
	_, found, err = service.WorkForSession("absent")
	if err != nil || found {
		t.Fatalf("absent work found=%v err=%v", found, err)
	}
}
