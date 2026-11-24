package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

func tReviews(t *testing.T, store *Store) []Work {
	t.Helper()
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	var out []Work
	for _, item := range items {
		if strings.HasPrefix(item.ID, tReviewPrefix) {
			out = append(out, item)
		}
	}
	return out
}

// Items in review raise one Work for Brain, biggest first, the whole list in
// a file; the same set is never asked twice, a new item is asked about at
// most once a day, and while the Work is open the new list is an Event on it.
func TestItemsInReviewRaiseOneWorkAndAreNeverAskedTwice(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	root := filepath.Dir(store.Root)
	item := func(i int) gc.Unproven {
		return gc.Unproven{Path: filepath.Join(root, "t", fmt.Sprintf("item%02d", i)), Bytes: int64(1000 * (i + 1)),
			LastWrite: clock.at().Add(-100 * time.Hour), Looks: "a cache", PreRule: i%2 == 0}
	}
	var set []gc.Unproven
	for i := 0; i < 25; i++ {
		set = append(set, item(i))
	}

	if err := service.raiseTReview(); err != nil || len(tReviews(t, store)) != 0 {
		t.Fatalf("nothing in review raised %d Works (%v)", len(tReviews(t, store)), err)
	}
	gc.SyncReviews(root, set, clock.at())
	for i := 0; i < 3; i++ {
		if err := service.raiseTReview(); err != nil {
			t.Fatal(err)
		}
	}
	works := tReviews(t, store)
	if len(works) != 1 || works[0].Title != "Review what t/ holds" {
		t.Fatalf("Works = %+v, want one", works)
	}
	obj := works[0].Objective
	if !strings.Contains(obj, "item24") || strings.Contains(obj, "item04:") || !strings.Contains(obj, "and 5 more") || !strings.Contains(obj, ".txt") {
		t.Fatalf("the Work names the biggest 20 and counts the rest: %q", obj)
	}
	if strings.Index(obj, "item24") > strings.Index(obj, "item23") {
		t.Fatalf("not the biggest first: %q", obj)
	}
	lists, _ := filepath.Glob(filepath.Join(store.TReviewDir(), "*.txt"))
	if len(lists) != 1 {
		t.Fatalf("list files = %v", lists)
	}
	if raw, _ := os.ReadFile(lists[0]); strings.Count(string(raw), "\n- ")+1 != 25 || !strings.Contains(string(raw), "there before the t/ rule") {
		t.Fatalf("the whole list is not in the file: %q", raw)
	}
	events, _ := store.ListWorkEvents(works[0].ID)
	if len(events) != 1 || events[0].Kind != tReviewKind {
		t.Fatalf("events = %+v", events)
	}
	if st, _ := store.fsmState(works[0].ID); st == nil || st.Review == nil {
		t.Fatalf("no review opened for Brain: %+v", st)
	}
	if _, due := gc.ReviewsDue(root, clock.at()); due {
		t.Fatal("the asked set is due again")
	}

	// A new item the same day: nothing. A day later, with the Work open: an
	// Event on it, not a second Work.
	set = append(set, item(25))
	gc.SyncReviews(root, set, clock.at())
	clock.advance(time.Hour)
	_ = service.raiseTReview()
	if got := tReviews(t, store); len(got) != 1 {
		t.Fatalf("a new item within a day: %d Works", len(got))
	}
	if events, _ = store.ListWorkEvents(works[0].ID); len(events) != 1 {
		t.Fatalf("a new item within a day: %d events", len(events))
	}
	clock.advance(24 * time.Hour)
	if err := service.raiseTReview(); err != nil {
		t.Fatal(err)
	}
	if got := tReviews(t, store); len(got) != 1 {
		t.Fatalf("with the Work open a day later: %d Works, want the one", len(got))
	}
	if events, _ = store.ListWorkEvents(works[0].ID); len(events) != 2 {
		t.Fatalf("with the Work open a day later: %d events, want 2", len(events))
	}

	// Closed with the items still unanswered: the same set is not asked again,
	// a new one is, in a new Work.
	current, _ := store.Work(works[0].ID)
	if _, err := service.CloseWork(WorkCloseRequest{WorkID: current.ID, ExpectedRevision: current.Revision, Status: WorkCancelled,
		Actor: lifecycle.ActorRuntime, Reason: "test"}); err != nil && err != ErrWorkCleanupPending {
		t.Fatal(err)
	}
	clock.advance(48 * time.Hour)
	_ = service.raiseTReview()
	if got := tReviews(t, store); len(got) != 1 {
		t.Fatalf("the same set after the Work closed: %d Works", len(got))
	}
	set = append(set, item(26))
	gc.SyncReviews(root, set, clock.at())
	if err := service.raiseTReview(); err != nil {
		t.Fatal(err)
	}
	if got := tReviews(t, store); len(got) != 2 {
		t.Fatalf("a new item after the Work closed: %d Works, want 2", len(got))
	}
}
