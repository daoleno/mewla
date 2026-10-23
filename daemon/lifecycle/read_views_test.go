package lifecycle

import "testing"

func TestFindReturnsACopyOfTheLowestMatchingID(t *testing.T) {
	e, _ := newTestEngine(t)
	for _, id := range []WorkID{"w3", "w1", "w2"} {
		define(t, e, id, PolicyUntilDone)
	}
	found, ok := e.Find(func(st *State) bool { return st.ID != "w1" })
	if !ok || found.ID != "w2" {
		t.Fatalf("Find = %v, %v; want w2", found, ok)
	}
	found.Title = "changed by the caller"
	again, _ := e.Find(func(st *State) bool { return st.ID == "w2" })
	if again.Title == "changed by the caller" {
		t.Fatal("Find handed out the engine's own state")
	}
	if _, ok := e.Find(func(*State) bool { return false }); ok {
		t.Fatal("Find reported a match nothing satisfies")
	}
}

func TestEachVisitsEveryWorkWithoutCopying(t *testing.T) {
	e, _ := newTestEngine(t)
	for _, id := range []WorkID{"w1", "w2", "w3"} {
		define(t, e, id, PolicyUntilDone)
	}
	seen := map[WorkID]bool{}
	e.Each(func(st *State) { seen[st.ID] = true })
	if len(seen) != 3 {
		t.Fatalf("Each visited %v", seen)
	}
	// A caller that obeys the contract allocates nothing per Work.
	allocs := testing.AllocsPerRun(50, func() { e.Each(func(*State) {}) })
	if allocs > 1 {
		t.Fatalf("Each allocates %.0f per call", allocs)
	}
}
