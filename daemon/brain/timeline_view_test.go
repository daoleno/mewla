package brain

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// referenceThreadTimeline is a thread's newest rows in chronological order,
// read the way a read did before the Store kept a view of each thread: every
// row copied, filtered, sorted.
func referenceThreadTimeline(t *testing.T, store *Store, threadID string, limit int) []TimelineItem {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	all, err := store.readAllTimelineItemsLocked()
	if err != nil {
		t.Fatal(err)
	}
	out := []TimelineItem{}
	for _, item := range all {
		item.ID, item.ThreadID = strings.TrimSpace(item.ID), strings.TrimSpace(item.ThreadID)
		if item.ThreadID == threadID {
			out = append(out, item)
		}
	}
	sortTimelineItems(out)
	if limit <= 0 {
		limit = defaultThreadTimelineLimit
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func appendTimelineRow(t *testing.T, store *Store, thread, id string, at time.Time) {
	t.Helper()
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: id, ThreadID: thread, SessionID: "session", Role: "assistant", Kind: timelineKindAssistantMessage,
		Body: "body " + id, CreatedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestThreadTimelineViewFollowsEveryChangeToTheTimeline(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	check := func(step string) {
		t.Helper()
		for _, thread := range []string{"thread-a", "thread-b", "missing", ""} {
			for _, limit := range []int{0, 1, 3, 500} {
				want := referenceThreadTimeline(t, store, thread, limit)
				if thread == "" {
					want = []TimelineItem{}
				}
				if got, err := store.ThreadTimeline(thread, limit); err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: ThreadTimeline(%q, %d) = %#v (%v), want %#v", step, thread, limit, got, err, want)
				}
				if got, err := store.ThreadTimelineView(thread, limit); err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: ThreadTimelineView(%q, %d) = %#v (%v), want %#v", step, thread, limit, got, err, want)
				}
			}
		}
	}
	check("empty")
	// Rows arrive out of chronological order and across threads.
	appendTimelineRow(t, store, "thread-a", "a3", base.Add(3*time.Minute))
	appendTimelineRow(t, store, "thread-b", "b1", base.Add(time.Minute))
	appendTimelineRow(t, store, "thread-a", "a1", base.Add(time.Minute))
	check("first rows")
	appendTimelineRow(t, store, "thread-a", "a2", base.Add(2*time.Minute))
	appendTimelineRow(t, store, "thread-a", "a0", base.Add(time.Minute)) // same time as a1: id breaks the tie
	check("a row between two others")

	// What a reader changes in what it was handed is its own.
	rows, err := store.ThreadTimeline("thread-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Body = "tampered"
	rows = rows[:1]
	check("after a caller edited its copy")

	// A rewrite of rows that are already stored replaces the view too.
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: "card", ThreadID: "thread-a", SessionID: "work", Role: "assistant", Kind: timelineKindWorkCard,
		Status: "session.done", EventKind: "session.done", WorkID: "work-1", Body: "card", Unread: true,
		CreatedAt: base.Add(4 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	check("a card")
	if err := store.MarkTimelineWorkCardsRead("work-1"); err != nil {
		t.Fatal(err)
	}
	check("a card read")
	view, err := store.ThreadTimelineView("thread-a", 0)
	if err != nil || view[len(view)-1].Unread {
		t.Fatalf("a read card is still unread in the view: %#v (%v)", view, err)
	}

	// The view of a thread whose rows did not change is not rebuilt.
	first, _ := store.ThreadTimelineView("thread-b", 0)
	second, _ := store.ThreadTimelineView("thread-b", 0)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("an unchanged thread was copied again")
	}
}

// A provider conversation the timeline already holds is checked against the
// thread's own rows: it reads and copies nothing the size of the whole
// timeline, however many rows other threads hold.
func TestMaterializeOfWhatTheTimelineHoldsCopiesNothingWhole(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const thread = "thread-a"
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	store.mu.Lock()
	for n := range 1500 {
		other := fmt.Sprintf("other-%d", n%20)
		if _, err := store.appendTimelineItemLocked(TimelineItem{
			ID: fmt.Sprintf("row-%d", n), ThreadID: other, SessionID: "session", Role: "assistant",
			Kind: timelineKindAssistantMessage, Body: strings.Repeat("x", 200), CreatedAt: base.Add(time.Duration(n) * time.Second),
		}); err != nil {
			store.mu.Unlock()
			t.Fatal(err)
		}
	}
	store.mu.Unlock()
	conversation := work.CodexConversation{Available: true, SessionID: "provider", Events: []work.CodexConversationEvent{{
		ID: "prompt", Kind: timelineKindUserMessage, Role: "user", Body: "a prompt",
		Timestamp: base.Add(-time.Minute).Format(time.RFC3339Nano),
	}}}
	for n := range 199 {
		conversation.Events = append(conversation.Events, work.CodexConversationEvent{
			ID: fmt.Sprintf("event-%d", n), Kind: timelineKindAssistantMessage, Role: "assistant",
			Body: "reply", Timestamp: base.Add(time.Duration(n) * time.Second).Format(time.RFC3339Nano),
		})
	}
	if err := store.MaterializeProviderConversation(thread, conversation); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.ThreadTimelineView(thread, 1000)
	if len(rows) != 200 {
		t.Fatalf("materialized %d rows, want 200", len(rows))
	}
	version := store.Version()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	const calls = 20
	for range calls {
		if err := store.MaterializeProviderConversation(thread, conversation); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	if store.Version() != version {
		t.Fatalf("materializing what is already stored wrote: version %d -> %d", version, store.Version())
	}
	// Copying the 1700 stored rows once is about 1 MB; the call may copy the
	// provider's own events and little more.
	if perCall := (after.TotalAlloc - before.TotalAlloc) / calls; perCall > 20<<10 {
		t.Fatalf("a materialize of stored rows allocated %d bytes per call", perCall)
	}
}

// What WorkResultLifecycles remembers of one presentation image never outlives
// it: each answer equals the one computed from nothing but the image.
func TestWorkResultLifecyclesFollowEveryCommittedChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	var ids []string
	check := func(step string) {
		t.Helper()
		got, err := store.WorkResultLifecycles(append(append([]string(nil), ids...), "unknown", " "))
		if err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		image := store.readPresentationLocked()
		latest := latestWorkResultEvents(image)
		want := map[string]WorkResultLifecycle{}
		for _, id := range ids {
			if lifecycle, ok := workResultLifecycle(image, latest, id); ok {
				want[id] = lifecycle
			}
		}
		store.mu.Unlock()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: lifecycles = %#v, want %#v", step, got, want)
		}
	}
	check("no Work")
	for n := range 3 {
		item, err := store.CreateWork(Work{
			Title: fmt.Sprintf("work %d", n), Objective: "o", Status: WorkRunning,
			CompletionPolicy: CompletionBounded, SourceThreadID: "thread",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
		check("created")
		for turn := range 2 {
			if _, _, err := service.AppendWorkEvent(WorkEvent{
				WorkID: item.ID, Kind: "session.done", Summary: "answer",
				DedupeKey: fmt.Sprintf("session:s%d:turn:%d:session.done", n, turn),
			}); err != nil {
				t.Fatal(err)
			}
			check("result event")
		}
	}
	done := WorkDone
	if _, err := service.UpdateWork(ids[0], WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	check("a Work finished")
	got, _ := store.WorkResultLifecycles(ids[:1])
	if got[ids[0]].SessionState != WorkResultSessionFinalized {
		t.Fatalf("a finished Work's result is %q, want finalized", got[ids[0]].SessionState)
	}
}
