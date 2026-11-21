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

func grantBatches(t *testing.T, store *Store) []Work {
	t.Helper()
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	var out []Work
	for _, item := range items {
		if strings.HasPrefix(item.ID, grantAnswersWorkPrefix) {
			out = append(out, item)
		}
	}
	return out
}

func sealedCard(t *testing.T, service *Service, permission, status string) TimelineItem {
	t.Helper()
	card, err := service.AskGrant(permission, "A Worker needs it.", service.nowUTC())
	if err != nil {
		t.Fatal(err)
	}
	card.Status = status
	return card
}

// The batch Work is read once: its event is opened for Brain only after a
// quiet spell, Brain reads it, and the runtime closes the Work as a fact, with
// no judge call.
func TestGrantAnswerReachesBrainOnceAndClosesWhenRead(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	line := "Accessibility is now allowed on this Mac. You can start the Worker again with the screen."
	if err := service.NoteGrantAnswered(sealedCard(t, service, "accessibility", GrantGranted), line); err != nil {
		t.Fatal(err)
	}
	batches := grantBatches(t, store)
	if len(batches) != 1 {
		t.Fatalf("batches %+v", batches)
	}
	events, _ := store.ListWorkEvents(batches[0].ID)
	if len(events) != 1 || events[0].Summary != line || events[0].Kind != grantAnswersEventKind {
		t.Fatalf("events %+v", events)
	}
	// Brain is not woken before the list has been quiet.
	clock.advance(grantAnswersQuiet - time.Second)
	if err := service.openGrantAnswers(); err != nil {
		t.Fatal(err)
	}
	if st, _ := store.fsmState(batches[0].ID); st.Review != nil {
		t.Fatal("Brain was woken before the quiet spell ended")
	}
	clock.advance(2 * time.Second)
	for i := 0; i < 2; i++ { // a second look changes nothing
		if err := service.openGrantAnswers(); err != nil {
			t.Fatal(err)
		}
	}
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	if claimed.WorkID != batches[0].ID || claimed.Kind != grantAnswersEventKind {
		t.Fatalf("Brain was handed %+v, want the answers", claimed)
	}
	input, err := marshalDirectWorkEventInput(claimed, batches[0])
	if err != nil || !strings.Contains(input, "Accessibility is now allowed") {
		t.Fatalf("Brain's input lacks the answer: %v %s", err, input)
	}
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	if _, ended, err := store.EndReviewDelivery(batches[0].ID, claimed.HandlingID, claimed.ProviderTurnID); err != nil || !ended {
		t.Fatalf("end handling = %v, %v", ended, err)
	}
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "x"}}
	service.SetWorkJudge(judge)
	clock.advance(settleDebounce + time.Minute)
	if err := service.settleWorkLocked(); err != nil {
		t.Fatal(err)
	}
	service.waitBackground()
	if got, _ := store.Work(batches[0].ID); got.Status != WorkDone || len(judge.briefs) != 0 {
		t.Fatalf("read answers = %s, judge calls %d", got.Status, len(judge.briefs))
	}
}

// Cards sealed close together arrive as one event, not one each: the newer
// list carries the older lines and the older, unread, is replaced.
func TestGrantAnswersSealedTogetherArriveAsOneEvent(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"screen_recording", "accessibility", "chrome"} {
		if err := service.NoteGrantAnswered(sealedCard(t, service, permission, GrantGranted), permission+" is now allowed."); err != nil {
			t.Fatal(err)
		}
		clock.advance(5 * time.Second)
	}
	batches := grantBatches(t, store)
	var open []Work
	for _, batch := range batches {
		if batch.Status != WorkCancelled {
			open = append(open, batch)
		}
	}
	if len(batches) != 3 || len(open) != 1 {
		t.Fatalf("batches %d, open %d, want 3 and 1 (the older ones replaced)", len(batches), len(open))
	}
	events, _ := store.ListWorkEvents(open[0].ID)
	if len(events) != 1 {
		t.Fatalf("events %+v", events)
	}
	for _, permission := range []string{"screen_recording", "accessibility", "chrome"} {
		if !strings.Contains(events[0].Summary, permission+" is now allowed.") {
			t.Fatalf("summary %q lacks %s", events[0].Summary, permission)
		}
	}
	// Only the newest was ever opened for Brain, and only once the quiet ended.
	clock.advance(grantAnswersQuiet)
	if err := service.openGrantAnswers(); err != nil {
		t.Fatal(err)
	}
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	if claimed.WorkID != open[0].ID {
		t.Fatalf("Brain was handed %+v", claimed)
	}
}

// An answer that comes while Brain reads the list is a list of its own after
// it; it never replaces the one being read, and one sealed after a read list
// is closed starts a new one.
func TestGrantAnswerDuringReadingWaitsForANewList(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := service.NoteGrantAnswered(sealedCard(t, service, "chrome", GrantGranted), "chrome is now allowed."); err != nil {
		t.Fatal(err)
	}
	clock.advance(grantAnswersQuiet + time.Second)
	if err := service.openGrantAnswers(); err != nil {
		t.Fatal(err)
	}
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	if claimed.Kind != grantAnswersEventKind {
		t.Fatalf("claimed %+v", claimed)
	}
	if err := service.NoteGrantAnswered(sealedCard(t, service, "accessibility", GrantDeclined), "accessibility: Not now."); err != nil {
		t.Fatal(err)
	}
	batches := grantBatches(t, store)
	if len(batches) != 2 {
		t.Fatalf("batches %d, want the one being read and a new one", len(batches))
	}
	for _, batch := range batches {
		if batch.Status == WorkCancelled {
			t.Fatalf("the list Brain is reading was replaced: %+v", batch)
		}
	}
}
