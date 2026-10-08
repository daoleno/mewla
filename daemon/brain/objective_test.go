package brain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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

func TestObjectiveCurrentHidesFinishedAndStaleGoals(t *testing.T) {
	set := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		objective Objective
		after     time.Duration
		want      bool
	}{
		{"just declared", Objective{}, time.Minute, true},
		{"declared, nothing handed off for half a day", Objective{}, 12 * time.Hour, false},
		{"Work out", Objective{Total: 3, Back: 1}, time.Hour, true},
		{"everything back, Brain still reading", Objective{Total: 3, Back: 3}, 10 * time.Minute, true},
		{"everything back and settled", Objective{Total: 3, Back: 3}, 30 * time.Minute, false},
		{"every child closed done", Objective{Total: 2, Back: 2, Done: 2}, time.Second, false},
		{"one straggler, nothing moved for half a day", Objective{Total: 27, Back: 26}, 12 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objective := tc.objective
			objective.Title = "Ship v1.4"
			objective.SetAt = set
			objective.LastActivity = set
			if got := objectiveCurrent(objective, set.Add(tc.after)); got != tc.want {
				t.Fatalf("objectiveCurrent(%+v, +%s) = %v, want %v", tc.objective, tc.after, got, tc.want)
			}
		})
	}
}

func TestDisplayedObjectiveLeavesTheAppsButNotBrain(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if _, err := store.SetObjective("Ship v1.4"); err != nil {
		t.Fatal(err)
	}
	work := createObjectiveWork(t, store, "sync fix")
	shown, err := store.DisplayedObjective()
	if err != nil || shown == nil || shown.Total != 1 {
		t.Fatalf("displayed = %#v, %v; want the objective with 1 child", shown, err)
	}

	now = now.Add(time.Hour)
	if _, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     work.ID,
		Kind:       "session.done",
		DedupeKey:  "session:sync:turn:one:session.done",
		Actionable: true,
		Summary:    "Fixed",
		PayloadRef: "session:s1",
	}); err != nil || !created {
		t.Fatalf("result event created=%v err=%v", created, err)
	}
	if shown, err := store.DisplayedObjective(); err != nil || shown == nil || shown.Back != 1 {
		t.Fatalf("just back: displayed = %#v, %v; want 1 of 1 back", shown, err)
	}

	now = now.Add(objectiveSettleAfter)
	if shown, err := store.DisplayedObjective(); err != nil || shown != nil {
		t.Fatalf("settled: displayed = %#v, %v; want hidden", shown, err)
	}
	if current, err := store.CurrentObjective(); err != nil || current == nil {
		t.Fatalf("Brain still sees its objective: %#v, %v", current, err)
	}
}

func TestObjectiveActivityIsHandoffsAndResultsNotRowTouches(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if _, err := store.SetObjective("Ship v1.4"); err != nil {
		t.Fatal(err)
	}
	work := createObjectiveWork(t, store, "straggler")

	// Something that is not a result touches the row late in the window.
	now = now.Add(11 * time.Hour)
	if _, _, err := store.AppendWorkEvent(WorkEvent{
		WorkID:    work.ID,
		Kind:      "brain.note",
		DedupeKey: "note:straggler:1",
		Summary:   "still looking",
	}); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentObjective()
	if err != nil || current == nil {
		t.Fatalf("current = %#v, %v", current, err)
	}
	if !current.LastActivity.Equal(work.CreatedAt) {
		t.Fatalf("last activity = %s, want the handoff at %s", current.LastActivity, work.CreatedAt)
	}

	now = work.CreatedAt.Add(objectiveStaleAfter)
	if shown, err := store.DisplayedObjective(); err != nil || shown != nil {
		t.Fatalf("stale: displayed = %#v, %v; want hidden", shown, err)
	}
}
