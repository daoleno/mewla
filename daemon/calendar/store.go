package calendar

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/statedir"
)

const SchemaVersion = 2

var errOwnerExists = errors.New("an item for this owner already exists")

type document struct {
	SchemaVersion int    `json:"schema_version"`
	Items         []Item `json:"items"`
}

type Event struct {
	Item            Item
	ScheduledResult *ScheduledResult
}

type Store struct {
	path string
	db   *sql.DB
	now  func() time.Time
	mu   sync.RWMutex
	// items is the resident calendar; stored is a copy of what was last
	// committed, so a commit can write only what changed (see db.go).
	items   map[string]Item
	stored  map[string]Item
	subs    map[int]chan Event
	nextSub int
	changed chan struct{}
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "calendar"), nil
}

func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("calendar root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	db, err := openRoot(root)
	if err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(root, databaseName), db: db, now: time.Now, items: map[string]Item{}, subs: map[int]chan Event{}, changed: make(chan struct{}, 1)}
	if err := s.load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	items, err := loadItems(s.db)
	if err != nil {
		return fmt.Errorf("load calendar: %w", err)
	}
	for id, item := range items {
		if id != "" {
			s.items[id] = item
		}
	}
	s.stored = cloneItems(s.items)
	return nil
}

func cloneItems(items map[string]Item) map[string]Item {
	out := make(map[string]Item, len(items))
	for id, item := range items {
		out[id] = cloneItem(item)
	}
	return out
}

func decodeDocument(raw []byte) (document, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return document{}, fmt.Errorf("calendar document must be a JSON object")
	}

	var encoded struct {
		SchemaVersion json.RawMessage `json:"schema_version"`
		Items         json.RawMessage `json:"items"`
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encoded); err != nil {
		return document{}, err
	}
	if trailing := bytes.TrimSpace(trimmed[decoder.InputOffset():]); len(trailing) != 0 {
		return document{}, fmt.Errorf("calendar document must contain exactly one JSON value")
	}

	if encoded.SchemaVersion == nil {
		return document{}, fmt.Errorf("schema_version is required")
	}
	if bytes.Equal(bytes.TrimSpace(encoded.SchemaVersion), []byte("null")) {
		return document{}, fmt.Errorf("schema_version must be a non-null integer")
	}
	var schemaVersion int
	if err := json.Unmarshal(encoded.SchemaVersion, &schemaVersion); err != nil {
		return document{}, fmt.Errorf("schema_version must be a non-null integer: %w", err)
	}
	if schemaVersion != SchemaVersion {
		return document{}, fmt.Errorf("schema_version must equal %d, got %d", SchemaVersion, schemaVersion)
	}

	if encoded.Items == nil {
		return document{}, fmt.Errorf("items is required")
	}
	if bytes.Equal(bytes.TrimSpace(encoded.Items), []byte("null")) {
		return document{}, fmt.Errorf("items must be a non-null JSON array")
	}
	var items []Item
	if err := json.Unmarshal(encoded.Items, &items); err != nil {
		return document{}, fmt.Errorf("items must be a JSON array: %w", err)
	}
	return document{SchemaVersion: schemaVersion, Items: items}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Subscribe() (int, <-chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan Event, 32)
	s.subs[id] = ch
	return id, ch
}
func (s *Store) Unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}
func (s *Store) broadcast(item Item) {
	s.broadcastEvent(Event{Item: item})
}

// Changed receives a value after any item changes. It holds at most one
// pending value, so a burst of changes is one wake; a receiver re-reads the
// store rather than counting them.
func (s *Store) Changed() <-chan struct{} { return s.changed }

func (s *Store) broadcastEvent(event Event) {
	select {
	case s.changed <- struct{}{}:
	default:
	}
	event.Item = cloneItem(event.Item)
	if event.ScheduledResult != nil {
		result := *event.ScheduledResult
		event.ScheduledResult = &result
	}
	for _, ch := range s.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Store) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, cloneItem(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextAt.Equal(out[j].NextAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].NextAt.Before(out[j].NextAt)
	})
	return out
}

// Running returns the scheduled actions that are launched and not finished.
func (s *Store) Running() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Item
	for _, item := range s.items {
		if item.Kind == KindScheduledAction && item.Status == StatusRunning {
			out = append(out, cloneItem(item))
		}
	}
	return out
}

func (s *Store) Get(id string) (Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return cloneItem(item), nil
}

// Create adds a client's item. Ownership is the daemon's to give, so a client
// cannot claim it.
func (s *Store) Create(item Item) (Item, error) {
	item.Owner = ""
	return s.create(item)
}

// Seed adds an item for owner unless one was ever created for it, whatever its
// status: a cancelled item stays, which is how deleting it turns its feature
// off for good. The check and the insert are one step under the lock, so a
// crash or a restart cannot leave two.
func (s *Store) Seed(owner string, item Item) (Item, bool, error) {
	if strings.TrimSpace(owner) == "" {
		return Item{}, false, fmt.Errorf("seed requires an owner")
	}
	item.Owner = owner
	created, err := s.create(item)
	if errors.Is(err, errOwnerExists) {
		return created, false, nil
	}
	return created, err == nil, err
}

func (s *Store) create(item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.Owner != "" {
		for _, existing := range s.items {
			if existing.Owner == item.Owner {
				return cloneItem(existing), errOwnerExists
			}
		}
	}
	item = cloneItem(item)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if _, exists := s.items[item.ID]; exists {
		return Item{}, fmt.Errorf("calendar id already exists")
	}
	if item.Recurrence == "" {
		item.Recurrence = RecurrenceNone
	}
	item.Summary = strings.TrimSpace(item.Summary)
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	now := s.now().UTC()
	item.CreatedAt, item.UpdatedAt, item.Revision = now, now, 1
	item.Status, item.NextAt = StatusScheduled, item.TriggerAt()
	item.Runs = nil
	item.SnoozedUntil, item.AlertedAt, item.AckedAt = nil, nil, nil
	s.items[item.ID] = item
	if err := s.persistLocked(); err != nil {
		delete(s.items, item.ID)
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) Update(item Item, expectedRevision int64) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.items[item.ID]
	if !ok {
		return Item{}, ErrNotFound
	}
	if expectedRevision > 0 && current.Revision != expectedRevision {
		return Item{}, ErrConflict
	}
	if current.Status == StatusRunning {
		return Item{}, fmt.Errorf("running scheduled action cannot be edited")
	}
	current = cloneItem(current)
	item = cloneItem(item)
	// Ownership stays with the daemon, whatever the client sent back.
	item.Owner = current.Owner
	if current.Owner != "" && item.Kind != KindReminder {
		return Item{}, fmt.Errorf("this item acts for Mewla and must stay a reminder")
	}
	if item.Recurrence == "" {
		item.Recurrence = RecurrenceNone
	}
	item.Summary = strings.TrimSpace(item.Summary)
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	item.CreatedAt, item.UpdatedAt, item.Revision = current.CreatedAt, s.now().UTC(), current.Revision+1
	item.Runs = append([]Run(nil), current.Runs...)
	item.Status, item.NextAt = StatusScheduled, item.TriggerAt()
	item.CancelledAt, item.FailureReason = nil, ""
	// The alert state is the daemon's, whatever the client sent back. Moving
	// the item drops its snooze and answers the alert it had open: the user is
	// already looking at it.
	item.SnoozedUntil, item.AlertedAt, item.AckedAt = current.SnoozedUntil, current.AlertedAt, current.AckedAt
	if !item.NextAt.Equal(current.NextAt) {
		item.SnoozedUntil = nil
		if item.AlertedAt != nil {
			acked := *item.AlertedAt
			item.AckedAt = &acked
		}
	}
	s.items[item.ID] = item
	if err := s.persistLocked(); err != nil {
		s.items[item.ID] = current
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) Cancel(id string, expectedRevision int64) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if expectedRevision > 0 && item.Revision != expectedRevision {
		return Item{}, ErrConflict
	}
	if item.Status == StatusRunning {
		return Item{}, fmt.Errorf("running scheduled action cannot be cancelled")
	}
	original := cloneItem(item)
	now := s.now().UTC()
	item.Status, item.CancelledAt, item.UpdatedAt = StatusCancelled, &now, now
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) SetStatus(id string, status Status, reason string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setStatusLocked(id, status, reason)
}

// CompleteOccurrence finishes one occurrence of an owned item and moves it to
// the next. It does nothing when the item was edited, cancelled or already
// advanced since occurrence was read, so a change made while the owner was
// acting is never overwritten.
func (s *Store) CompleteOccurrence(id string, occurrence time.Time) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, false, ErrNotFound
	}
	if item.Status != StatusScheduled || !item.NextAt.Equal(occurrence) {
		return cloneItem(item), false, nil
	}
	done, err := s.setStatusLocked(id, StatusCompleted, "")
	return done, err == nil, err
}

func (s *Store) setStatusLocked(id string, status Status, reason string) (Item, error) {
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	item.Status, item.FailureReason, item.UpdatedAt = status, reason, s.now().UTC()
	item.Revision++
	if status == StatusCompleted && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	}
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

// MarkAlerted records that the alert due at alertAt fired, which opens it for
// the user and spends a snooze. It does nothing, reporting false, when the
// item no longer alerts at that instant (it was edited, snoozed or cancelled
// since alertAt was read), so a stale pass never marks a later alert as sent.
func (s *Store) MarkAlerted(id string, alertAt time.Time) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, false, ErrNotFound
	}
	pending, due := item.AlertPending()
	if !due || !pending.Equal(alertAt) {
		return cloneItem(item), false, nil
	}
	return s.updateLocked(id, func(item *Item) {
		fired := alertAt
		item.AlertedAt, item.SnoozedUntil = &fired, nil
	})
}

// Snooze moves the item's next alert to until and answers the alert that is
// open. A finished or cancelled item has nothing to snooze.
func (s *Store) Snooze(id string, until time.Time) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if !until.After(s.now()) {
		return Item{}, fmt.Errorf("snooze must end in the future")
	}
	if item.Owner != "" || item.Kind == KindScheduledAction {
		return Item{}, fmt.Errorf("this item does not alert")
	}
	switch item.Status {
	case StatusScheduled, StatusWaiting, StatusRunning:
	default:
		return Item{}, fmt.Errorf("a %s item cannot be snoozed", item.Status)
	}
	updated, _, err := s.updateLocked(id, func(item *Item) {
		until := until.UTC()
		item.SnoozedUntil = &until
		item.AckedAt = cloneTimePtr(item.AlertedAt)
	})
	return updated, err
}

// Done answers the item's open alert. A reminder or deadline is finished
// (a repeating one moves to its next time); an event only stops alerting, it
// is not over until it ends.
func (s *Store) Done(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	switch item.Status {
	case StatusScheduled, StatusWaiting, StatusRunning:
	default:
		return cloneItem(item), nil
	}
	switch item.Kind {
	case KindReminder, KindDeadline:
		if item.Owner != "" {
			return Item{}, fmt.Errorf("this item acts for Mewla and cannot be marked done")
		}
		// A repeating reminder moves on the moment it is due, before the user
		// has answered: its open alert then belongs to the occurrence behind
		// it, and finishing the next one would skip it.
		if item.Status == StatusWaiting || (item.AlertedAt != nil && !item.AlertedAt.Before(item.NextAt.Add(-item.AlertLead()))) {
			if _, err := s.setStatusLocked(id, StatusCompleted, ""); err != nil {
				return Item{}, err
			}
		}
		return s.ackLocked(id)
	case KindEvent:
		return s.ackLocked(id)
	}
	return Item{}, fmt.Errorf("a scheduled action cannot be marked done")
}

// ackLocked answers the open alert and drops a snooze, bumping the revision
// only when something changed.
func (s *Store) ackLocked(id string) (Item, error) {
	item := s.items[id]
	if item.SnoozedUntil == nil && (item.AlertedAt == nil || (item.AckedAt != nil && !item.AckedAt.Before(*item.AlertedAt))) {
		return cloneItem(item), nil
	}
	updated, _, err := s.updateLocked(id, func(item *Item) {
		item.SnoozedUntil = nil
		item.AckedAt = cloneTimePtr(item.AlertedAt)
	})
	return updated, err
}

// updateLocked applies change to an item as one committed revision.
func (s *Store) updateLocked(id string, change func(*Item)) (Item, bool, error) {
	original := s.items[id]
	item := cloneItem(original)
	change(&item)
	item.UpdatedAt = s.now().UTC()
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, false, err
	}
	s.broadcast(item)
	return cloneItem(item), true, nil
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *Store) Claim(id string, manual bool) (Item, Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, Run{}, ErrNotFound
	}
	if item.Kind != KindScheduledAction {
		return Item{}, Run{}, fmt.Errorf("run is only valid for scheduled_action")
	}
	if item.Status == StatusRunning {
		return Item{}, Run{}, ErrClaimed
	}
	if item.Status == StatusCancelled {
		return Item{}, Run{}, fmt.Errorf("cancelled action cannot run")
	}
	original := cloneItem(item)
	item = cloneItem(item)
	scheduledFor := item.NextAt
	if manual {
		scheduledFor = s.now().UTC()
	}
	for _, previous := range item.Runs {
		if !manual && previous.ScheduledFor.Equal(scheduledFor) {
			return Item{}, Run{}, ErrClaimed
		}
	}
	now := s.now().UTC()
	run := Run{
		ID:             uuid.NewString(),
		Title:          strings.TrimSpace(item.Title),
		SourceThreadID: strings.TrimSpace(item.SourceThreadID),
		ScheduledFor:   scheduledFor,
		StartedAt:      now,
		Status:         StatusRunning,
		Manual:         manual,
		PreviousStatus: item.Status,
	}
	item.Runs = append(item.Runs, run)
	item.Status, item.UpdatedAt, item.FailureReason = StatusRunning, now, ""
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, Run{}, err
	}
	s.broadcast(item)
	return cloneItem(item), run, nil
}

func (s *Store) RecordLaunch(id, runID, workID, workerSession string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	idx := -1
	for n := range item.Runs {
		if item.Runs[n].ID == runID {
			idx = n
			break
		}
	}
	if idx < 0 {
		return Item{}, fmt.Errorf("calendar run not found")
	}
	if item.Runs[idx].Status != StatusRunning {
		return Item{}, ErrClaimed
	}
	now := s.now().UTC()
	item.Runs[idx].WorkID, item.Runs[idx].WorkerSession = workID, workerSession
	item.LinkedWorkID, item.Status, item.FailureReason, item.UpdatedAt = workID, StatusRunning, "", now
	item.Revision++
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	s.broadcast(item)
	return cloneItem(item), nil
}

func (s *Store) FinishRun(id, runID, result, failure string) (Item, error) {
	status, result, failure, err := normalizeTerminalPayload(result, failure)
	if err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	original := cloneItem(item)
	item = cloneItem(item)
	idx := -1
	for n := range item.Runs {
		if item.Runs[n].ID == runID {
			idx = n
			break
		}
	}
	if idx < 0 {
		return Item{}, fmt.Errorf("calendar run not found")
	}
	if item.Runs[idx].Status != StatusRunning {
		return cloneItem(item), nil
	}
	now := s.now().UTC()
	item.Runs[idx].Status, item.Runs[idx].FinishedAt = status, &now
	item.Runs[idx].Result = result
	item.Runs[idx].FailureReason = failure
	item.Status, item.FailureReason, item.UpdatedAt = status, failure, now
	item.Revision++
	if item.Runs[idx].Manual && item.Runs[idx].PreviousStatus == StatusScheduled && item.NextAt.After(now) {
		item.Status = StatusScheduled
		item.FailureReason = ""
	} else if item.Runs[idx].Manual && item.Recurrence != RecurrenceNone && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	} else if !item.Runs[idx].Manual && advanceItem(&item) {
		for attempts := 0; attempts < 400 && !item.NextAt.After(s.now()); attempts++ {
			advanceItem(&item)
		}
	}
	// A failure belongs to its run. The item keeps it only when the failed
	// run ended the item; a series that goes on is scheduled, not failed.
	if item.Status != StatusFailed {
		item.FailureReason = ""
	}
	s.items[id] = item
	if err := s.persistLocked(); err != nil {
		s.items[id] = original
		return Item{}, err
	}
	event := Event{Item: item}
	if result, ok := scheduledResultFromRun(id, item.Runs[idx]); ok {
		event.ScheduledResult = &result
	}
	s.broadcastEvent(event)
	return cloneItem(item), nil
}

func normalizeTerminalPayload(result, failure string) (Status, string, string, error) {
	hasResult := strings.TrimSpace(result) != ""
	hasFailure := strings.TrimSpace(failure) != ""
	if !hasResult && !hasFailure {
		return "", "", "", fmt.Errorf("scheduled run requires exactly one result or failure")
	}
	if hasResult && hasFailure {
		return "", "", "", fmt.Errorf("scheduled run cannot contain both result and failure")
	}
	if hasResult {
		normalized, err := normalizeScheduledResult(result)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid scheduled result: %w", err)
		}
		return StatusCompleted, normalized, "", nil
	}
	normalized, err := normalizeScheduledFailure(failure)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid scheduled failure: %w", err)
	}
	return StatusFailed, "", normalized, nil
}

// persistLocked commits what changed since the last commit. Callers hold
// s.mu and restore the Item they changed when it fails.
func (s *Store) persistLocked() error {
	if err := commit(s.db, s.stored, s.items); err != nil {
		return err
	}
	for id, item := range s.items {
		if !reflect.DeepEqual(s.stored[id], item) {
			s.stored[id] = cloneItem(item)
		}
	}
	for id := range s.stored {
		if _, kept := s.items[id]; !kept {
			delete(s.stored, id)
		}
	}
	return nil
}
