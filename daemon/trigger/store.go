package trigger

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// Triggers live in <root>/triggers.db, one row per Trigger as its JSON (a
// Trigger keeps at most KeepFirings firings, so a row stays small). Each
// Trigger's payload files are under <root>/<id>/.
const databaseName = "triggers.db"

var schemaSteps = []string{`
CREATE TABLE trg_item (
	id   TEXT PRIMARY KEY,
	body TEXT NOT NULL
) WITHOUT ROWID;
`}

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Store keeps every Trigger resident and commits each change as one row.
type Store struct {
	root    string
	db      *sql.DB
	now     func() time.Time
	mu      sync.Mutex
	items   map[string]Trigger
	changed chan struct{}
	subsMu  sync.Mutex
	subs    map[int]chan Trigger
	nextSub int
}

func OpenStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("trigger root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	db, err := sqlitedb.Open(filepath.Join(root, databaseName))
	if err != nil {
		return nil, err
	}
	if err := sqlitedb.Migrate(db, "trigger", schemaSteps); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{root: root, db: db, now: time.Now, items: map[string]Trigger{}, changed: make(chan struct{}, 1), subs: map[int]chan Trigger{}}
	rows, err := db.Query(`SELECT id, body FROM trg_item`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			_ = db.Close()
			return nil, err
		}
		var item Trigger
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("trigger %s: %w", id, err)
		}
		s.items[id] = item
	}
	if err := rows.Err(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Root() string { return s.root }

// Dir is where a Trigger's payload files live.
func (s *Store) Dir(id string) string { return filepath.Join(s.root, id) }

// Changed signals after every committed change.
func (s *Store) Changed() <-chan struct{} { return s.changed }

// Subscribe receives every committed Trigger (a deleted one with Revision 0
// and only its ID set).
func (s *Store) Subscribe() (int, <-chan Trigger) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.nextSub++
	ch := make(chan Trigger, 32)
	s.subs[s.nextSub] = ch
	return s.nextSub, ch
}

func (s *Store) Unsubscribe(id int) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}

func (s *Store) notify(item Trigger) {
	select {
	case s.changed <- struct{}{}:
	default:
	}
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- item:
		default:
		}
	}
}

func (s *Store) List() []Trigger {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Trigger, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, clone(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (s *Store) Get(id string) (Trigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return Trigger{}, ErrNotFound
	}
	return clone(item), nil
}

// Create stores a normalized Trigger with a new ID. The caller has already
// bound WorkID.
func (s *Store) Create(item Trigger) (Trigger, error) {
	item, err := item.Normalize()
	if err != nil {
		return Trigger{}, err
	}
	if item.WorkID == "" {
		return Trigger{}, fmt.Errorf("%w: the Trigger has no Work", ErrInvalid)
	}
	now := s.now().UTC()
	item.ID = uuid.NewString()
	item.CreatedAt, item.UpdatedAt, item.Revision = now, now, 1
	if err := os.MkdirAll(filepath.Join(s.Dir(item.ID), "fires"), 0o700); err != nil {
		return Trigger{}, err
	}
	s.mu.Lock()
	err = s.writeLocked(item)
	if err == nil {
		s.items[item.ID] = item
	}
	s.mu.Unlock()
	if err != nil {
		return Trigger{}, err
	}
	s.notify(clone(item))
	return clone(item), nil
}

// Update changes one Trigger in place and commits it when change reports a
// change. Revision moves only for a change the person made (bump).
func (s *Store) Update(id string, change func(*Trigger) (changed bool, bump bool, err error)) (Trigger, error) {
	s.mu.Lock()
	current, ok := s.items[id]
	if !ok {
		s.mu.Unlock()
		return Trigger{}, ErrNotFound
	}
	next := clone(current)
	changed, bump, err := change(&next)
	if err != nil || !changed {
		s.mu.Unlock()
		return clone(current), err
	}
	next.UpdatedAt = s.now().UTC()
	if bump {
		next.Revision++
	}
	if err := s.writeLocked(next); err != nil {
		s.mu.Unlock()
		return clone(current), err
	}
	s.items[id] = next
	s.mu.Unlock()
	s.notify(clone(next))
	return clone(next), nil
}

// Delete removes a Trigger and its payload directory.
func (s *Store) Delete(id string) (Trigger, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	current, ok := s.items[id]
	if !ok {
		s.mu.Unlock()
		return Trigger{}, ErrNotFound
	}
	err := sqlitedb.Tx(s.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM trg_item WHERE id = ?`, id)
		return err
	})
	if err == nil {
		delete(s.items, id)
	}
	s.mu.Unlock()
	if err != nil {
		return Trigger{}, err
	}
	if idPattern.MatchString(id) {
		_ = os.RemoveAll(s.Dir(id))
	}
	s.notify(Trigger{ID: id})
	return current, nil
}

func (s *Store) writeLocked(item Trigger) error {
	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return sqlitedb.Tx(s.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO trg_item(id, body) VALUES(?, ?)
			ON CONFLICT(id) DO UPDATE SET body = excluded.body`, item.ID, string(body))
		return err
	})
}

// clone deep-copies through JSON's own shape: every stored value is the
// decode of what was committed.
func clone(item Trigger) Trigger {
	body, err := json.Marshal(item)
	if err != nil {
		return item
	}
	var out Trigger
	if json.Unmarshal(body, &out) != nil {
		return item
	}
	return out
}

// HasWork says whether a Trigger reports into workID.
func (s *Store) HasWork(workID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.WorkID == workID {
			return true
		}
	}
	return false
}
