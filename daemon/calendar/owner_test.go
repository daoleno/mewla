package calendar

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type ownerRecorder struct {
	mu   sync.Mutex
	dues []time.Time
	err  error
}

func (r *ownerRecorder) fire(_ Item, due time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.dues = append(r.dues, due)
	return nil
}

func (r *ownerRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.dues)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const briefWindow = 3*time.Hour + 30*time.Minute

func ownedScheduler(t *testing.T, store *Store, now *time.Time, rec *ownerRecorder) *Scheduler {
	t.Helper()
	scheduler := NewScheduler(store, nil)
	scheduler.now = func() time.Time { return *now }
	scheduler.Own(OwnerWatchBrief, OwnerDuty{Window: briefWindow, Fire: rec.fire})
	return scheduler
}

func briefItem(first time.Time) Item {
	return Item{Title: "Morning brief", Kind: KindReminder, NotifyAt: &first, Timezone: "Asia/Shanghai", Recurrence: RecurrenceDaily}
}

func TestSeedCreatesOneItemForOwnerAndNeverAnother(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	created, seeded, err := store.Seed(OwnerWatchBrief, briefItem(first))
	if err != nil || !seeded || created.Owner != OwnerWatchBrief {
		t.Fatalf("seed = %#v %v %v", created, seeded, err)
	}
	if _, seeded, err := store.Seed(OwnerWatchBrief, briefItem(first)); err != nil || seeded {
		t.Fatalf("second seed = %v %v", seeded, err)
	}
	// Cancelling is how the user turns the brief off: it must stay off.
	if _, err := store.Cancel(created.ID, created.Revision); err != nil {
		t.Fatal(err)
	}
	if again, seeded, err := store.Seed(OwnerWatchBrief, briefItem(first)); err != nil || seeded || again.ID != created.ID {
		t.Fatalf("seed after cancel = %#v %v %v", again, seeded, err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, seeded, err := reopened.Seed(OwnerWatchBrief, briefItem(first)); err != nil || seeded {
		t.Fatalf("seed after restart = %v %v", seeded, err)
	}
	if got := len(reopened.List()); got != 1 {
		t.Fatalf("items = %d", got)
	}
}

func TestClientsCannotClaimOrChangeOwnership(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	forged := briefItem(first)
	forged.Owner = OwnerWatchBrief
	created, err := store.Create(forged)
	if err != nil || created.Owner != "" {
		t.Fatalf("a client claimed ownership: %#v %v", created, err)
	}
	owned, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	edit := owned
	edit.Owner = ""
	edit.Title = "早报"
	edited, err := store.Update(edit, owned.Revision)
	if err != nil || edited.Owner != OwnerWatchBrief || edited.Title != "早报" {
		t.Fatalf("edit = %#v %v", edited, err)
	}
	edit = edited
	edit.Kind = KindDeadline
	due := first
	edit.DueAt, edit.NotifyAt = &due, nil
	if _, err := store.Update(edit, edited.Revision); err == nil {
		t.Fatal("an owned item changed kind")
	}
}

func TestOwnedReminderFiresOnceThenMovesToTomorrow(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(-time.Hour)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	rec := &ownerRecorder{}
	scheduler := ownedScheduler(t, store, &now, rec)
	ctx := context.Background()
	scheduler.Tick(ctx)
	if rec.count() != 0 {
		t.Fatal("fired before it was due")
	}
	now = first.Add(time.Second)
	scheduler.Tick(ctx)
	eventually(t, "the brief to fire", func() bool { return rec.count() == 1 })
	eventually(t, "the item to move on", func() bool {
		got, _ := store.Get(item.ID)
		return got.NextAt.Equal(first.AddDate(0, 0, 1)) && got.Status == StatusScheduled
	})
	scheduler.Tick(ctx)
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 1 || !rec.dues[0].Equal(first) {
		t.Fatalf("fired %v", rec.dues)
	}
	// It is a reminder the user can see: never "needs you".
	if got, _ := store.Get(item.ID); got.Status == StatusWaiting {
		t.Fatal("owned reminder waits for the user")
	}
}

func TestEditingTheTimeMovesWhenItFires(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(-time.Hour)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	rec := &ownerRecorder{}
	scheduler := ownedScheduler(t, store, &now, rec)
	moved := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 7, 0)
	edit := item
	edit.NotifyAt = &moved
	if _, err := store.Update(edit, item.Revision); err != nil {
		t.Fatal(err)
	}
	now = moved.Add(time.Minute)
	scheduler.Tick(context.Background())
	eventually(t, "the moved brief to fire", func() bool { return rec.count() == 1 })
	if !rec.dues[0].Equal(moved) {
		t.Fatalf("fired for %v, want %v", rec.dues[0], moved)
	}
	eventually(t, "tomorrow at 07:00", func() bool {
		got, _ := store.Get(item.ID)
		return got.NextAt.Equal(moved.AddDate(0, 0, 1))
	})
	// 08:30 passes with nothing more to fire.
	now = first.Add(time.Minute)
	scheduler.Tick(context.Background())
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("fired at the old time: %v", rec.dues)
	}
}

func TestCancellingStopsTheBrief(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(-time.Hour)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	rec := &ownerRecorder{}
	scheduler := ownedScheduler(t, store, &now, rec)
	if _, err := store.Cancel(item.ID, item.Revision); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 3; day++ {
		now = first.AddDate(0, 0, day).Add(time.Minute)
		scheduler.Tick(context.Background())
	}
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("a cancelled brief fired %d times", rec.count())
	}
}

func TestRestartAfterFiringButBeforeAdvancingFiresTheSameOccurrence(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(time.Minute)
	store.now = func() time.Time { return now }
	store.Seed(OwnerWatchBrief, briefItem(first))
	// The daemon dies after Fire succeeded and before the item advanced.
	crashed := &ownerRecorder{}
	crashing := NewScheduler(store, nil)
	crashing.now = func() time.Time { return now }
	crashing.Own(OwnerWatchBrief, OwnerDuty{Window: briefWindow, Fire: func(item Item, due time.Time) error {
		_ = crashed.fire(item, due)
		return errors.New("daemon died")
	}})
	crashing.Tick(context.Background())
	eventually(t, "the first attempt", func() bool { return crashed.count() == 1 })

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now }
	rec := &ownerRecorder{}
	scheduler := ownedScheduler(t, reopened, &now, rec)
	scheduler.Tick(context.Background())
	eventually(t, "the retry after restart", func() bool { return rec.count() == 1 })
	// The owner is told the same instant, which is what it dedupes on.
	if !rec.dues[0].Equal(crashed.dues[0]) {
		t.Fatalf("occurrence changed across a restart: %v vs %v", rec.dues[0], crashed.dues[0])
	}
	eventually(t, "advance", func() bool {
		items := reopened.List()
		return len(items) == 1 && items[0].NextAt.Equal(first.AddDate(0, 0, 1))
	})
	if got := len(reopened.List()); got != 1 {
		t.Fatalf("items after restart = %d", got)
	}
}

func TestFailedFireRetriesWithBackoffAndSkipsOnceTooLate(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(time.Minute)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	rec := &ownerRecorder{err: errors.New("Brain is busy")}
	scheduler := ownedScheduler(t, store, &now, rec)
	scheduler.Tick(context.Background())
	time.Sleep(30 * time.Millisecond)
	scheduler.Tick(context.Background()) // inside the backoff: not tried
	time.Sleep(30 * time.Millisecond)
	if got, _ := store.Get(item.ID); !got.NextAt.Equal(first) {
		t.Fatal("a failed brief moved on")
	}
	rec.mu.Lock()
	rec.err = nil
	rec.mu.Unlock()
	now = now.Add(ownerRetry + time.Second)
	scheduler.Tick(context.Background())
	eventually(t, "the retry", func() bool { return rec.count() == 1 })
	eventually(t, "advance", func() bool {
		got, _ := store.Get(item.ID)
		return got.NextAt.After(first)
	})

	// The daemon was off all night: this morning's brief is too late to be
	// useful, so the occurrence is skipped without firing.
	late := &ownerRecorder{}
	store2, _ := NewStore(t.TempDir())
	store2.Seed(OwnerWatchBrief, briefItem(first))
	now2 := first.Add(briefWindow + time.Minute)
	store2.now = func() time.Time { return now2 }
	scheduler2 := ownedScheduler(t, store2, &now2, late)
	scheduler2.Tick(context.Background())
	time.Sleep(30 * time.Millisecond)
	items := store2.List()
	if late.count() != 0 || !items[0].NextAt.Equal(first.AddDate(0, 0, 1)) {
		t.Fatalf("late occurrence: fired %d, next %v", late.count(), items[0].NextAt)
	}
}

func TestEditDuringFireIsNotOverwritten(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(time.Minute)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	moved := first.AddDate(0, 0, 2)
	rec := &ownerRecorder{}
	scheduler := NewScheduler(store, nil)
	scheduler.now = func() time.Time { return now }
	scheduler.Own(OwnerWatchBrief, OwnerDuty{Window: briefWindow, Fire: func(i Item, due time.Time) error {
		edit := i
		edit.NotifyAt = &moved
		if _, err := store.Update(edit, i.Revision); err != nil {
			return err
		}
		return rec.fire(i, due)
	}})
	scheduler.Tick(context.Background())
	eventually(t, "the fire", func() bool { return rec.count() == 1 })
	time.Sleep(30 * time.Millisecond)
	if got, _ := store.Get(item.ID); !got.NextAt.Equal(moved) {
		t.Fatalf("the user's edit was overwritten: next %v", got.NextAt)
	}
}

func TestOwnedItemWithoutAnOwnerIsLeftAlone(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	first := localTime(t, "Asia/Shanghai", 2026, time.October, 23, 8, 30)
	now := first.Add(time.Minute)
	store.now = func() time.Time { return now }
	item, _, _ := store.Seed(OwnerWatchBrief, briefItem(first))
	scheduler := NewScheduler(store, nil)
	scheduler.now = func() time.Time { return now }
	scheduler.Tick(context.Background())
	if got, _ := store.Get(item.ID); got.Status != StatusScheduled || !got.NextAt.Equal(first) {
		t.Fatalf("item changed without an owner: %#v", got)
	}
}
