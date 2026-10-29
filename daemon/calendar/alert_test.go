package calendar

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type alertRecorder struct {
	mu    sync.Mutex
	items []Item
	ats   []time.Time
	err   error
}

func (r *alertRecorder) send(item Item, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
	r.ats = append(r.ats, at)
	return r.err
}

func (r *alertRecorder) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *alertRecorder) at(index int) (Item, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.items[index], r.ats[index]
}

func (r *alertRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

type alertFixture struct {
	t         *testing.T
	root      string
	store     *Store
	scheduler *Scheduler
	rec       *alertRecorder
	mu        sync.Mutex
	now       time.Time
}

func (f *alertFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *alertFixture) setClock(at time.Time) {
	f.mu.Lock()
	f.now = at
	f.mu.Unlock()
}

func newAlertFixture(t *testing.T) *alertFixture {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	f := &alertFixture{t: t, root: root, store: store, rec: &alertRecorder{}, now: localTime(t, "America/New_York", 2026, time.July, 14, 9, 0)}
	store.now = f.clock
	f.scheduler = NewScheduler(store, nil)
	f.scheduler.now = f.clock
	f.scheduler.Alert(f.rec.send)
	return f
}

// tick runs one pass at now and waits for the sends it started.
func (f *alertFixture) tick(at time.Time) {
	f.t.Helper()
	f.setClock(at)
	f.scheduler.Tick(context.Background())
	time.Sleep(20 * time.Millisecond)
}

func (f *alertFixture) event(start time.Time, lead *int) Item {
	f.t.Helper()
	end := start.Add(time.Hour)
	item, err := f.store.Create(Item{Title: "Standup", Kind: KindEvent, StartAt: &start, EndAt: &end, Timezone: "America/New_York", AlertLeadMinutes: lead})
	if err != nil {
		f.t.Fatal(err)
	}
	return item
}

func intPtr(v int) *int { return &v }

func TestDueReminderAndEventReachTheAlerter(t *testing.T) {
	f := newAlertFixture(t)
	rem, _ := f.store.Create(reminder(f.now.Add(time.Minute)))
	event := f.event(f.now.Add(30*time.Minute), nil)
	f.tick(f.now)
	if f.rec.count() != 0 {
		t.Fatalf("alerted before its time: %d", f.rec.count())
	}
	f.tick(f.now.Add(time.Minute))
	if f.rec.count() < 1 || firstID(f.rec) != rem.ID {
		t.Fatalf("reminder was not alerted at its time")
	}
	f.tick(f.now.Add(19 * time.Minute)) // 9:20, the event's default lead of 10 min is 9:20
	if f.rec.count() < 2 || secondID(f.rec) != event.ID {
		t.Fatalf("event was not alerted 10 min ahead")
	}
	if want := event.NextAt.Add(-10 * time.Minute); !secondAt(f.rec).Equal(want) {
		t.Fatalf("event alert at %s, want %s", secondAt(f.rec), want)
	}
}

func TestAlertLeadIsPerItemAndDefaultsPerKind(t *testing.T) {
	f := newAlertFixture(t)
	start := f.now.Add(2 * time.Hour)
	custom := f.event(start, intPtr(30))
	due := start
	deadline, _ := f.store.Create(Item{Title: "Ship", Kind: KindDeadline, DueAt: &due, Timezone: "America/New_York"})
	f.tick(start.Add(-31 * time.Minute))
	if f.rec.count() != 0 {
		t.Fatal("alerted before the lead")
	}
	f.tick(start.Add(-30 * time.Minute))
	if f.rec.count() != 2 {
		t.Fatalf("custom 30 min event and default 30 min deadline: got %d", f.rec.count())
	}
	got := map[string]bool{firstID(f.rec): true, secondID(f.rec): true}
	if !got[custom.ID] || !got[deadline.ID] {
		t.Fatalf("alerted %v", got)
	}
	if _, err := f.store.Create(Item{Title: "x", Kind: KindEvent, StartAt: &start, EndAt: &due, Timezone: "UTC", AlertLeadMinutes: intPtr(-1)}); err == nil {
		t.Fatal("negative lead was accepted")
	}
}

func TestAlertFiresOncePerOccurrenceEvenAcrossRestart(t *testing.T) {
	f := newAlertFixture(t)
	rem, _ := f.store.Create(reminder(f.now.Add(time.Minute)))
	f.tick(f.now.Add(time.Minute))
	f.tick(f.now.Add(2 * time.Minute))
	f.tick(f.now.Add(3 * time.Minute))
	if f.rec.count() != 1 {
		t.Fatalf("alerts = %d, want 1", f.rec.count())
	}
	// A daemon reload: a new store and scheduler over the same state.
	store, err := NewStore(f.root)
	if err != nil {
		t.Fatal(err)
	}
	rec := &alertRecorder{}
	again := NewScheduler(store, nil)
	again.now = func() time.Time { return f.now }
	again.Alert(rec.send)
	again.Tick(context.Background())
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("a reload alerted %s again", rem.ID)
	}
}

func TestRecurringReminderAlertsEveryDay(t *testing.T) {
	f := newAlertFixture(t)
	at := f.now.Add(time.Minute)
	item, _ := f.store.Create(Item{Title: "Stretch", Kind: KindReminder, NotifyAt: &at, Timezone: "America/New_York", Recurrence: RecurrenceDaily})
	f.tick(at)
	if f.rec.count() != 1 {
		t.Fatalf("day 1 alerts = %d", f.rec.count())
	}
	moved, _ := f.store.Get(item.ID)
	if !moved.NextAt.After(at) || !moved.AlertOpen() {
		t.Fatalf("recurring reminder = next %s open %v", moved.NextAt, moved.AlertOpen())
	}
	f.tick(at.Add(24 * time.Hour))
	if f.rec.count() != 2 {
		t.Fatalf("day 2 alerts = %d", f.rec.count())
	}
}

func TestAlertMissedWhileDaemonWasOffIsDropped(t *testing.T) {
	f := newAlertFixture(t)
	f.store.Create(reminder(f.now.Add(time.Minute)))
	f.tick(f.now.Add(time.Minute + AlertWindow + time.Second))
	if f.rec.count() != 0 {
		t.Fatal("a stale alert was sent")
	}
}

func TestSnoozeMovesTheAlertAndDoneEndsIt(t *testing.T) {
	f := newAlertFixture(t)
	rem, _ := f.store.Create(reminder(f.now.Add(time.Minute)))
	f.tick(f.now.Add(time.Minute))
	fired := f.now
	snoozed, err := f.store.Snooze(rem.ID, fired.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snoozed.AlertOpen() {
		t.Fatal("a snoozed alert is still open")
	}
	f.tick(fired.Add(4 * time.Minute))
	if f.rec.count() != 1 {
		t.Fatalf("alerted during the snooze: %d", f.rec.count())
	}
	f.tick(fired.Add(5 * time.Minute))
	if f.rec.count() != 2 {
		t.Fatalf("snooze did not alert again: %d", f.rec.count())
	}
	again, _ := f.store.Get(rem.ID)
	if again.SnoozedUntil != nil || !again.AlertOpen() {
		t.Fatalf("after the snooze: %#v", again)
	}
	done, err := f.store.Done(rem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusCompleted || done.AlertOpen() {
		t.Fatalf("done = %s open %v", done.Status, done.AlertOpen())
	}
	f.tick(fired.Add(time.Hour))
	if f.rec.count() != 2 {
		t.Fatalf("a done reminder alerted again: %d", f.rec.count())
	}
}

func TestDoneOnAnEventOnlyStopsItsAlert(t *testing.T) {
	f := newAlertFixture(t)
	event := f.event(f.now.Add(15*time.Minute), nil)
	f.tick(f.now.Add(5 * time.Minute))
	done, err := f.store.Done(event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusScheduled || done.AlertOpen() {
		t.Fatalf("event after done = %s open %v", done.Status, done.AlertOpen())
	}
}

func TestDoneOnARepeatingReminderThatAlreadyMovedOnKeepsTomorrow(t *testing.T) {
	f := newAlertFixture(t)
	at := f.now.Add(time.Minute)
	item, _ := f.store.Create(Item{Title: "Stretch", Kind: KindReminder, NotifyAt: &at, Timezone: "America/New_York", Recurrence: RecurrenceDaily})
	f.tick(at)
	tomorrow, _ := f.store.Get(item.ID)
	done, err := f.store.Done(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !done.NextAt.Equal(tomorrow.NextAt) || done.Status != StatusScheduled || done.AlertOpen() {
		t.Fatalf("done skipped tomorrow: next %s status %s", done.NextAt, done.Status)
	}
}

func TestEditingTheTimeDropsSnoozeAndAnswersTheAlert(t *testing.T) {
	f := newAlertFixture(t)
	rem, _ := f.store.Create(reminder(f.now.Add(time.Minute)))
	f.tick(f.now.Add(time.Minute))
	moved := rem
	later := f.now.Add(3 * time.Hour)
	moved.NotifyAt = &later
	updated, err := f.store.Update(moved, 0)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AlertOpen() || updated.AlertedAt == nil {
		t.Fatalf("edited item = %#v", updated)
	}
	f.tick(later)
	if f.rec.count() != 2 {
		t.Fatalf("the moved time did not alert: %d", f.rec.count())
	}
}

func TestClientCannotSetTheAlertState(t *testing.T) {
	f := newAlertFixture(t)
	at := f.now.Add(time.Hour)
	forged := reminder(at)
	forged.AlertedAt, forged.SnoozedUntil = &at, &at
	created, err := f.store.Create(forged)
	if err != nil {
		t.Fatal(err)
	}
	if created.AlertedAt != nil || created.SnoozedUntil != nil {
		t.Fatalf("created = %#v", created)
	}
}

func TestFailedAlertIsRetriedButNoTargetIsNot(t *testing.T) {
	f := newAlertFixture(t)
	f.rec.fail(errors.New("network"))
	f.store.Create(reminder(f.now.Add(time.Minute)))
	first := f.now.Add(time.Minute)
	f.tick(first)
	f.tick(first.Add(30 * time.Second))
	if f.rec.count() != 1 {
		t.Fatalf("retried too soon: %d", f.rec.count())
	}
	f.tick(first.Add(ownerRetry))
	if f.rec.count() != 2 {
		t.Fatalf("not retried after %s: %d", ownerRetry, f.rec.count())
	}

	g := newAlertFixture(t)
	g.rec.fail(ErrNoAlertTarget)
	g.store.Create(reminder(g.now.Add(time.Minute)))
	g.tick(g.now.Add(time.Minute))
	g.tick(g.now.Add(3 * time.Minute))
	if g.rec.count() != 1 {
		t.Fatalf("no target was retried: %d", g.rec.count())
	}
}

func TestOwnedItemsAndActionsNeverAlert(t *testing.T) {
	f := newAlertFixture(t)
	first := f.now.Add(time.Minute)
	f.store.Seed(OwnerWatchBrief, briefItem(first))
	f.tick(first)
	if f.rec.count() != 0 {
		t.Fatal("owned item alerted")
	}
}

func firstID(r *alertRecorder) string  { item, _ := r.at(0); return item.ID }
func secondID(r *alertRecorder) string { item, _ := r.at(1); return item.ID }
func secondAt(r *alertRecorder) time.Time {
	_, at := r.at(1)
	return at
}
