package brain

import (
	"reflect"
	"testing"
	"time"
)

// The read paths hand out the committed image's rows without copying the whole
// image first. What they return is still the caller's own: changing it changes
// nothing the store holds.
func TestReadPathsReturnCopiesOfTheCommittedImage(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "brain_thread_read_copy"}); err != nil {
		t.Fatal(err)
	}
	soon := time.Now().UTC().Add(time.Hour)
	item, err := store.CreateWork(Work{
		Title: "Copies", Objective: "Reading is not writing", Status: WorkRunning, CompletionPolicy: CompletionBounded,
		Choices: []string{"Ship", "Wait"}, QuestionAt: &soon, SnoozedUntil: &soon,
		Wake: &WorkWake{Kind: "due_retry", Ref: "wake", NextAttemptAt: &soon},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, _, err := store.AppendWorkEvent(WorkEvent{WorkID: item.ID, Kind: "note", DedupeKey: "read-copy:1", Summary: "first"})
	if err != nil {
		t.Fatal(err)
	}

	works, err := store.ListWork()
	if err != nil || len(works) == 0 {
		t.Fatalf("works = %v, err = %v", works, err)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events = %v, err = %v", events, err)
	}
	wantWorks, wantEvents := cloneWorks(works), append([]WorkEvent(nil), events...)
	for index := range wantEvents {
		wantEvents[index] = cloneWorkEvent(wantEvents[index])
	}

	// Scribble over everything the caller was handed.
	for index := range works {
		works[index].Title = "changed"
		for choice := range works[index].Choices {
			works[index].Choices[choice] = "changed"
		}
		if works[index].QuestionAt != nil {
			*works[index].QuestionAt = time.Unix(1, 0)
		}
		if works[index].SnoozedUntil != nil {
			*works[index].SnoozedUntil = time.Unix(1, 0)
		}
		if works[index].Wake != nil {
			works[index].Wake.Ref = "changed"
			if works[index].Wake.NextAttemptAt != nil {
				*works[index].Wake.NextAttemptAt = time.Unix(1, 0)
			}
		}
	}
	for index := range events {
		events[index].Summary = "changed"
		for _, at := range []*time.Time{events[index].HandledAt, events[index].ResolvedAt, events[index].DiscardedAt} {
			if at != nil {
				*at = time.Unix(1, 0)
			}
		}
	}

	againWorks, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	againEvents, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(againWorks, wantWorks) {
		t.Fatalf("a caller changed the store's Work:\n got %+v\nwant %+v", againWorks, wantWorks)
	}
	if !reflect.DeepEqual(againEvents, wantEvents) || againEvents[0].ID != event.ID {
		t.Fatalf("a caller changed the store's events:\n got %+v\nwant %+v", againEvents, wantEvents)
	}
}
