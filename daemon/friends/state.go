package friends

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	// MaxPendingDrafts stops the Brain writing more while a person has not
	// looked at these.
	MaxPendingDrafts = 5
	// DraftTTL is how long an unhandled draft is kept.
	DraftTTL = 7 * 24 * time.Hour
)

// ErrNotLinked means Friends is off on this computer.
var ErrNotLinked = errors.New("Friends is not set up on this computer; turn it on in the app (Settings, Labs)")

// ErrTooManyDrafts means the person has drafts waiting.
var ErrTooManyDrafts = fmt.Errorf("%d drafts are already waiting for the owner; write no more until they are handled", MaxPendingDrafts)

// State is what this computer remembers while Friends is on. It exists only
// then: turning Friends off removes it with the drafts.
type State struct {
	Handle    string    `json:"handle"`
	EnabledAt time.Time `json:"enabled_at"`
	// NotifySeq is the newest Friends notification already pushed to the phone.
	NotifySeq int64 `json:"notify_seq"`
	// DraftsPushed is how many drafts the phone was last told were waiting.
	DraftsPushed int `json:"drafts_pushed"`
}

// Draft is a post waiting for its owner. It lives on this computer only.
type Draft struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store keeps the state and drafts under <state dir>/friends.
type Store struct {
	Dir string
	Now func() time.Time
}

func NewStore(stateDir string) *Store {
	return &Store{Dir: filepath.Join(stateDir, "friends")}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) statePath() string { return filepath.Join(s.Dir, "state.json") }

func (s *Store) ensure() error { return os.MkdirAll(filepath.Join(s.Dir, "drafts"), 0o700) }

// SaveState records that Friends is on.
func (s *Store) SaveState(state State) error {
	if err := s.ensure(); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.statePath(), raw, 0o600)
}

// State returns the saved state, or ErrNotLinked when Friends is off.
func (s *Store) State() (State, error) {
	raw, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotLinked
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil || state.Handle == "" {
		return State{}, ErrNotLinked
	}
	return state, nil
}

// Clear forgets the state and every draft.
func (s *Store) Clear() error { return os.RemoveAll(s.Dir) }

// AddDraft stores a draft that already passed the scrubber.
func (s *Store) AddDraft(text, source string) (Draft, error) {
	if _, err := s.State(); err != nil {
		return Draft{}, err
	}
	pending, err := s.Drafts()
	if err != nil {
		return Draft{}, err
	}
	if len(pending) >= MaxPendingDrafts {
		return Draft{}, ErrTooManyDrafts
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return Draft{}, err
	}
	draft := Draft{ID: hex.EncodeToString(raw), Text: text, Source: source, CreatedAt: s.now().UTC()}
	encoded, err := json.Marshal(draft)
	if err != nil {
		return Draft{}, err
	}
	if err := s.ensure(); err != nil {
		return Draft{}, err
	}
	return draft, atomicfile.Write(filepath.Join(s.Dir, "drafts", draft.ID+".json"), encoded, 0o600)
}

// Drafts lists waiting drafts, oldest first, and deletes the expired.
func (s *Store) Drafts() ([]Draft, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "drafts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var drafts []Draft
	for _, entry := range entries {
		path := filepath.Join(s.Dir, "drafts", entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var draft Draft
		if json.Unmarshal(raw, &draft) != nil || draft.ID == "" {
			continue
		}
		if s.now().Sub(draft.CreatedAt) > DraftTTL {
			_ = os.Remove(path)
			continue
		}
		drafts = append(drafts, draft)
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].CreatedAt.Before(drafts[j].CreatedAt) })
	return drafts, nil
}

// Draft returns one waiting draft.
func (s *Store) Draft(id string) (Draft, error) {
	drafts, err := s.Drafts()
	if err != nil {
		return Draft{}, err
	}
	for _, draft := range drafts {
		if draft.ID == id {
			return draft, nil
		}
	}
	return Draft{}, ErrNoDraft
}

// RemoveDraft discards a draft; one that is already gone is not an error.
func (s *Store) RemoveDraft(id string) error {
	if !validDraftID(id) {
		return ErrNoDraft
	}
	err := os.Remove(filepath.Join(s.Dir, "drafts", id+".json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ErrNoDraft means there is no such draft.
var ErrNoDraft = errors.New("no such draft")

func validDraftID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
