package brain

import "testing"

// Every unfinished Work reaches the app's rail, even when no Worker has it
// and it sits past the bounded current window; finished Work never does.
func TestOpenWorkListsUnfinishedWorkOutsideCurrent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created := map[string]bool{}
	for _, title := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		item, err := store.CreateWork(Work{Title: title, Objective: "ship " + title, CompletionPolicy: CompletionBounded})
		if err != nil {
			t.Fatal(err)
		}
		created[item.ID] = true
	}
	finished, err := store.CreateWork(Work{Title: "finished", Objective: "done already", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	done := WorkDone
	if _, err := store.UpdateWork(finished.ID, WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}

	inventory, err := store.ProjectWorkInventory(nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range inventory.Current {
		seen[item.ID] = true
	}
	for _, item := range inventory.Open {
		if seen[item.ID] {
			t.Fatalf("Work %s is in both current and open", item.ID)
		}
		if item.Status == WorkDone || item.Status == WorkCancelled {
			t.Fatalf("finished Work %s listed as open", item.ID)
		}
		if item.Title == "" || item.Status == "" {
			t.Fatalf("open Work needs its title and status: %+v", item)
		}
		seen[item.ID] = true
	}
	for id := range created {
		if !seen[id] {
			t.Fatalf("unfinished Work %s is in neither current nor open", id)
		}
	}
	if seen[finished.ID] {
		t.Fatalf("finished Work must not be listed")
	}
	if len(inventory.Open) == 0 {
		t.Fatalf("seven ready Works exceed the current window; the rest must be open")
	}
}
