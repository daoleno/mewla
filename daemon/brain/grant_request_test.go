package brain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func grantTestService(t *testing.T) (*Store, *Service) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, newTestService(t, store, &fakeWatcher{}, nil)
}

// A grant request is a card in Brain's thread: the reason, the permission and
// where it stands; a change of its status reaches followers (the store's
// version moves, the event carries the new status) and the card is never made twice.
func TestGrantRequestCardAsEventAndStatusChange(t *testing.T) {
	store, service := grantTestService(t)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	card, err := service.AskGrant("screen_recording", "  A Worker needs to see\nthe screen. ", now)
	if err != nil {
		t.Fatal(err)
	}
	if card.Status != GrantAsked || card.Kind != timelineKindGrantRequest || card.Body != "A Worker needs to see the screen." ||
		card.ID != "grant:screen_recording:"+strings.TrimPrefix(card.ID, "grant:screen_recording:") {
		t.Fatalf("card %+v", card)
	}
	threadID, _ := store.ChatThreadID()
	items, err := store.ThreadTimeline(threadID, defaultThreadTimelineLimit)
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, event := range TimelineItemsToConversationEvents(items) {
		if event.Source == "grant_request" {
			if event.Kind != "status" || event.Role != "assistant" || event.GrantPermission != "screen_recording" || event.Body != card.Body {
				t.Fatalf("event %+v", event)
			}
			shown = append(shown, event.Status)
		}
	}
	if len(shown) != 1 || shown[0] != "asked" {
		t.Fatalf("shown %v", shown)
	}

	before := store.Version()
	updated, err := store.UpdateGrantRequest(card.ID, func(item *TimelineItem) error {
		item.Status = GrantOpened
		return nil
	})
	if err != nil || updated.Status != GrantOpened {
		t.Fatalf("update %+v, %v", updated, err)
	}
	if store.Version() == before {
		t.Fatal("the store's version did not move: followers are not pushed a status change")
	}
	cards, _ := store.GrantRequests()
	if len(cards) != 1 || cards[0].Status != GrantOpened {
		t.Fatalf("cards %+v", cards)
	}
	event := GrantRequestEvent(cards[0])
	if event.Status != "opened" || event.Source != "grant_request" || event.ID != card.ID {
		t.Fatalf("event %+v", event)
	}
	// A refusing change writes nothing.
	refuse := errors.New("no")
	if _, err := store.UpdateGrantRequest(card.ID, func(item *TimelineItem) error { item.Status = GrantGranted; return refuse }); !errors.Is(err, refuse) {
		t.Fatalf("err %v", err)
	}
	if cards, _ = store.GrantRequests(); cards[0].Status != GrantOpened {
		t.Fatalf("a refused change was written: %+v", cards[0])
	}
	if _, err := store.UpdateGrantRequest("grant:none", func(*TimelineItem) error { return nil }); !errors.Is(err, ErrGrantRequestNotFound) {
		t.Fatalf("err %v", err)
	}
	if _, err := service.AskGrant("chrome", " ", now); err == nil {
		t.Fatal("a card without a reason")
	}
}

// How a card ended reaches Brain as one event on a Work of its own, once.
func TestNoteGrantAnsweredRaisesOneEvent(t *testing.T) {
	store, service := grantTestService(t)
	card, err := service.AskGrant("accessibility", "A Worker needs to click and type.", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	card.Status = GrantGranted
	line := "Accessibility is now allowed on this Mac. You can start the Worker again with the screen."
	for i := 0; i < 2; i++ {
		if err := service.NoteGrantAnswered(card, line); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListWorkEvents("grant-request:" + card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Summary != line || events[0].Kind != "grant_request" || !events[0].Actionable {
		t.Fatalf("events %+v", events)
	}
}
