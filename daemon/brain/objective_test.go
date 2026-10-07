package brain

import (
	"encoding/json"
	"strings"
	"testing"
)

func newObjectiveStore(t *testing.T, threadID string) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: threadID, ThreadIDs: []string{threadID}}); err != nil {
		t.Fatal(err)
	}
	return store
}

func createObjectiveWork(t *testing.T, store *Store, title string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{
		Title:            title,
		Objective:        title,
		Status:           WorkRunning,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestObjectiveCountsThreadWorkCreatedSinceItWasSet(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	createObjectiveWork(t, store, "before the objective")

	set, err := store.SetObjective("  Ship   atlas-notes v1.4 this week ")
	if err != nil {
		t.Fatal(err)
	}
	if set.Title != "Ship atlas-notes v1.4 this week" || set.Total != 0 || set.Back != 0 {
		t.Fatalf("set objective = %#v", set)
	}

	sync := createObjectiveWork(t, store, "sync fix")
	createObjectiveWork(t, store, "settings copy")
	if _, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     sync.ID,
		Kind:       "session.done",
		DedupeKey:  "session:sync:turn:one:session.done",
		Actionable: true,
		Summary:    "Fixed",
		PayloadRef: "session:s1",
	}); err != nil || !created {
		t.Fatalf("result event created=%v err=%v", created, err)
	}

	current, err := store.CurrentObjective()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.Total != 2 || current.Back != 1 {
		t.Fatalf("objective progress = %#v, want 1 of 2 back", current)
	}
	if !current.SetAt.Equal(set.SetAt) {
		t.Fatalf("set_at drifted: %s vs %s", current.SetAt, set.SetAt)
	}
}

func TestObjectiveRedeclaringKeepsStartAndNewTitleRestarts(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	first, err := store.SetObjective("Ship v1.4")
	if err != nil {
		t.Fatal(err)
	}
	createObjectiveWork(t, store, "part one")
	again, err := store.SetObjective("Ship v1.4")
	if err != nil {
		t.Fatal(err)
	}
	if !again.SetAt.Equal(first.SetAt) || again.Total != 1 {
		t.Fatalf("re-declared objective = %#v, want same start and its Work", again)
	}
	next, err := store.SetObjective("Ship v1.5")
	if err != nil {
		t.Fatal(err)
	}
	if next.Total != 0 {
		t.Fatalf("new objective counted earlier Work: %#v", next)
	}
}

func TestObjectiveBelongsToItsChatThread(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	if _, err := store.SetObjective("Ship v1.4"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-b", ThreadIDs: []string{"thread-a", "thread-b"}}); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentObjective()
	if err != nil || current != nil {
		t.Fatalf("new chat inherited objective %#v err=%v", current, err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-a", ThreadIDs: []string{"thread-a", "thread-b"}}); err != nil {
		t.Fatal(err)
	}
	if current, err := store.CurrentObjective(); err != nil || current == nil {
		t.Fatalf("objective lost on return to its thread: %#v err=%v", current, err)
	}
}

func TestObjectiveClearAndValidation(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	if _, err := store.SetObjective("   "); err == nil {
		t.Fatal("blank objective accepted")
	}
	if _, err := store.SetObjective(strings.Repeat("x", maxObjectiveTitleRunes+1)); err == nil {
		t.Fatal("over-long objective accepted")
	}
	if _, err := store.SetObjective("Ship v1.4"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearObjective(); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearObjective(); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
	if current, err := store.CurrentObjective(); err != nil || current != nil {
		t.Fatalf("cleared objective = %#v err=%v", current, err)
	}
}

func TestObjectiveSkipsDaemonOwnedWork(t *testing.T) {
	if isObjectiveChildWork(calendarWorkID("item", "run")) {
		t.Fatal("Calendar occurrence counted as objective Work")
	}
	if isObjectiveChildWork(resourcePressureWorkPrefix + "20261007T125540Z") {
		t.Fatal("resource telemetry counted as objective Work")
	}
	if !isObjectiveChildWork("6cbcd6cc-f1a4-4579-9b64-b69f2f5d4a24") {
		t.Fatal("delegated Work not counted")
	}
}

func TestSnapshotWireCarriesObjectiveOnlyWhenSet(t *testing.T) {
	encode := func(snapshot Snapshot) map[string]any {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	if _, ok := encode(Snapshot{})["objective"]; ok {
		t.Fatal("snapshot without an objective must omit the field")
	}
	payload := encode(Snapshot{Objective: &Objective{Title: "Ship v1.4", Total: 5, Back: 2}})
	objective, ok := payload["objective"].(map[string]any)
	if !ok || objective["title"] != "Ship v1.4" || objective["total"] != float64(5) || objective["back"] != float64(2) {
		t.Fatalf("objective wire = %#v", payload["objective"])
	}
}
