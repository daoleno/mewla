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

// ErrNotLinked means this computer has not been linked to a Friends account.
var ErrNotLinked = errors.New("Friends is not set up on this computer; turn it on in the app (Settings, Labs)")

// ErrTooManyDrafts means the person has drafts waiting.
var ErrTooManyDrafts = fmt.Errorf("%d drafts are already waiting for the owner; write no more until they are handled", MaxPendingDrafts)

// Link is what this computer remembers of its Friends account.
type Link struct {
	Handle     string    `json:"handle"`
	ServiceURL string    `json:"service_url"`
	LinkedAt   time.Time `json:"linked_at"`
}

// Draft is a post waiting for its owner. It lives on this computer only.
type Draft struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store keeps the link and drafts under <state dir>/friends.
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

func (s *Store) linkPath() string { return filepath.Join(s.Dir, "link.json") }

func (s *Store) ensure() error { return os.MkdirAll(filepath.Join(s.Dir, "drafts"), 0o700) }

// SaveLink records the account this computer is linked to.
func (s *Store) SaveLink(link Link) error {
	if err := s.ensure(); err != nil {
		return err
	}
	raw, err := json.Marshal(link)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.linkPath(), raw, 0o600)
}

// Link returns the account link, or ErrNotLinked.
func (s *Store) Link() (Link, error) {
	raw, err := os.ReadFile(s.linkPath())
	if errors.Is(err, os.ErrNotExist) {
		return Link{}, ErrNotLinked
	}
	if err != nil {
		return Link{}, err
	}
	var link Link
	if err := json.Unmarshal(raw, &link); err != nil || link.Handle == "" {
		return Link{}, ErrNotLinked
	}
	return link, nil
}

// Clear forgets the link and every draft.
func (s *Store) Clear() error { return os.RemoveAll(s.Dir) }

// AddDraft stores a draft that already passed the scrubber.
func (s *Store) AddDraft(text, source string) (Draft, error) {
	if _, err := s.Link(); err != nil {
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
