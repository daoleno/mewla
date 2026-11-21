package brain

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Where a grant request card stands (its timeline item's Status). Asked and
// opened are waiting on the person, restart on Mewla (the grant exists but
// does not count in this process yet); granted and declined seal the card.
const (
	GrantAsked    = "asked"
	GrantOpened   = "opened"
	GrantRestart  = "restart"
	GrantGranted  = "granted"
	GrantDeclined = "declined"
)

// GrantOpen says whether a card still waits (it is not sealed).
func GrantOpen(status string) bool {
	switch status {
	case GrantAsked, GrantOpened, GrantRestart:
		return true
	}
	return false
}

// ErrGrantRequestNotFound: no grant request card has that id.
var ErrGrantRequestNotFound = errors.New("no such grant request")

// AskGrant writes a grant request card into Brain's thread (status asked).
// The reason is one line. The caller says whether a card for the permission is
// already open: nothing here deduplicates.
func (s *Service) AskGrant(permission, reason string, now time.Time) (TimelineItem, error) {
	threadID, err := s.store.ChatThreadID()
	if err != nil {
		return TimelineItem{}, err
	}
	if strings.TrimSpace(threadID) == "" {
		return TimelineItem{}, errors.New("Brain has no Chat thread to ask in")
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return TimelineItem{}, errors.New("a grant request needs a reason")
	}
	host, _ := s.store.HostSession()
	return s.store.AppendTimelineItem(TimelineItem{
		ID:              "grant:" + permission + ":" + strconv.FormatInt(now.UnixNano(), 10),
		ThreadID:        threadID,
		SessionID:       firstNonEmpty(host.ID, "grants"),
		ExecutorID:      host.ExecutorID,
		Role:            "assistant",
		Kind:            timelineKindGrantRequest,
		Status:          GrantAsked,
		Body:            reason,
		GrantPermission: permission,
		CreatedAt:       now,
	})
}

// GrantRequests are every grant request card, oldest first.
func (s *Store) GrantRequests() ([]TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return nil, err
	}
	var out []TimelineItem
	for _, item := range items {
		if item.Kind == timelineKindGrantRequest {
			out = append(out, item)
		}
	}
	return out, nil
}

// UpdateGrantRequest changes one grant request card under the store's lock:
// change sees the card as stored and may refuse (its error is returned, nothing
// is written). A change that alters the card is pushed to the Brain thread's
// followers like any timeline change.
func (s *Store) UpdateGrantRequest(id string, change func(*TimelineItem) error) (TimelineItem, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return TimelineItem{}, err
	}
	for index := range items {
		if items[index].ID != id || items[index].Kind != timelineKindGrantRequest {
			continue
		}
		if err := change(&items[index]); err != nil {
			return items[index], err
		}
		updated := items[index]
		return updated, s.rewriteTimelineLocked(items)
	}
	return TimelineItem{}, ErrGrantRequestNotFound
}

// GrantRequestEvent is a card as the conversation feed carries it.
func GrantRequestEvent(item TimelineItem) work.CodexConversationEvent {
	event, _ := timelineItemToConversationEvent(item, 0)
	return event
}

// NoteGrantAnswered tells Brain, once per card, how a grant request ended, in
// one line, through the same lane a Work's events take: a Work of its own
// (bounded, so Brain closes it once it has acted) with one actionable event.
func (s *Service) NoteGrantAnswered(card TimelineItem, line string) error {
	if s == nil || s.store == nil {
		return errors.New("brain service is not configured")
	}
	workID := "grant-request:" + card.ID
	if _, _, err := s.store.EnsureWork(Work{
		ID:               workID,
		Title:            "Permission answered: " + card.GrantPermission,
		Objective:        "Act on the person's answer to a permission request.",
		CompletionPolicy: CompletionBounded,
		NextAction:       "Start the Worker again with the tool if it was granted; stop asking for this task if the person chose Not now; then close this.",
	}); err != nil {
		return fmt.Errorf("grant request Work: %w", err)
	}
	_, _, err := s.RaiseWorkEvent(WorkEvent{
		WorkID: workID, Kind: "grant_request", DedupeKey: card.ID + ":" + card.Status,
		SourceName: "Mewla", Summary: line, Actionable: true,
	})
	if err != nil {
		log.Printf("grant request %s: telling Brain: %v", card.ID, err)
	}
	return err
}
