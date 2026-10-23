package calendar

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	log.SetOutput(out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return out
}

// A due reminder whose status change cannot be persisted must be visible, and
// a repeat of the same failure must not write a line per pass.
func TestSchedulerReportsAStatusChangeThatCannotBePersistedOnce(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := localTime(t, "America/New_York", 2026, time.July, 14, 9, 0)
	store.now = func() time.Time { return now }
	at := now.Add(time.Minute)
	item, err := store.Create(reminder(at))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	out := captureLog(t)
	scheduler := NewScheduler(store, nil)
	scheduler.now = func() time.Time { return at.Add(time.Second) }
	for range 3 {
		scheduler.Tick(context.Background())
	}
	lines := strings.Count(out.String(), "WARN calendar set-status "+item.ID)
	if lines != 1 {
		t.Fatalf("expected the failure once, got %d:\n%s", lines, out.String())
	}
}

func TestSchedulerReportsATerminalRunThatCannotBePersisted(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := localTime(t, "America/New_York", 2026, time.July, 14, 10, 0)
	store.now = func() time.Time { return now }
	due := now.Add(time.Hour)
	item, err := store.Create(Item{Title: "Report", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Run it", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, run, err := store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	out := captureLog(t)
	scheduler := NewScheduler(store, nil)
	scheduler.finish(claimed, run, "done", "")
	scheduler.finish(claimed, run, "done", "")
	if n := strings.Count(out.String(), "WARN calendar finish-run "+item.ID); n != 1 {
		t.Fatalf("expected the failure once, got %d:\n%s", n, out.String())
	}
}

func TestSchedulerStaysQuietWhenALaunchRaceIsLost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := captureLog(t)
	scheduler := NewScheduler(store, nil)
	scheduler.note("calendar run x", ErrClaimed)
	if out.String() != "" {
		t.Fatalf("ErrClaimed is a lost race, not a failure:\n%s", out.String())
	}
}
