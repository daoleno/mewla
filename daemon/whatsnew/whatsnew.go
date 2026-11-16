// Package whatsnew keeps what is news to this person after Mewla changed
// version: the releases' notes (release-notes/<version>.json, bundled here by
// scripts/release-notes.py) and the record of what they have seen.
//
// The record lives on the computer so that a highlight seen on the phone is
// not shown again on the web. It is one JSON file in the state directory:
//
//   - baseline: the version Mewla was installed at; nothing at or below it is
//     news. Empty for a Mewla that was in use before it kept this record, so
//     every release with notes counts.
//   - seen: the highlight ids the person has seen, on any device. Seeing adds
//     ids; it never replaces the list, so the phone and the web cannot undo
//     each other.
//   - updates: one entry per version this Mewla reached, the computer's own
//     update (auto-update included) or the app's, for the line it leaves in
//     Brain's chat.
//   - replay: set by `mewla dev whats-new`, which also clears seen and the
//     baseline; the app shows the sheet once as for a Mewla from before any
//     release with notes, whatever the versions say, then it is cleared.
package whatsnew

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/selfupdate"
)

//go:embed notes.json
var notesJSON []byte

// Notes is every release's notes, newest first, as the app reads them.
func Notes() json.RawMessage { return json.RawMessage(notesJSON) }

// FileName is the record's name in the state directory.
const FileName = "whats-new.json"

// maxSeen bounds the record: a few highlights a release for years.
const maxSeen = 2048

// Update is one version this person's Mewla reached.
type Update struct {
	Version string `json:"version"`
	// At is milliseconds since the epoch.
	At int64 `json:"at"`
}

// Record is what the app reads.
type Record struct {
	Baseline string   `json:"baseline"`
	Seen     []string `json:"seen"`
	Updates  []Update `json:"updates"`
	Replay   bool     `json:"replay,omitempty"`
}

type file struct {
	Record
	// LastVersion is the version of the daemon that last ran.
	LastVersion string `json:"last_version"`
}

// Store is the record of one computer.
type Store struct {
	mu   sync.Mutex
	path string
	data file
}

// Open reads the record and notes this daemon's version. A computer without
// a record is a fresh install when nobody has paired with it yet (existing
// is false): its own version is the baseline. One that was in use before
// this record existed counts every release with notes, and its version is
// an update, so the first update onto this feature leaves its line too.
func Open(stateDir, version string, existing bool, now time.Time) (*Store, error) {
	s := &Store{path: filepath.Join(stateDir, FileName)}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(raw, &s.data); jsonErr != nil {
			// A record that cannot be read is started again as an existing install.
			s.data = file{}
			existing = true
			err = os.ErrNotExist
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	changed := false
	if errors.Is(err, os.ErrNotExist) {
		changed = true
		if existing {
			s.data.Baseline = ""
			s.data.Updates = []Update{{Version: version, At: now.UnixMilli()}}
		} else {
			s.data.Baseline = version
		}
		s.data.LastVersion = version
	} else if s.data.LastVersion != version {
		changed = true
		if newer(version, s.data.LastVersion) {
			s.addUpdateLocked(version, now)
		}
		s.data.LastVersion = version
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

func validVersion(version string) bool {
	return len(version) <= 32 && versionPattern.MatchString(version)
}

func newer(version, than string) bool {
	if than == "" {
		return true
	}
	return selfupdate.Older(than, version)
}

func (s *Store) addUpdateLocked(version string, now time.Time) bool {
	if version == "" || (s.data.Baseline != "" && !newer(version, s.data.Baseline)) {
		return false
	}
	for _, u := range s.data.Updates {
		if u.Version == version {
			return false
		}
	}
	s.data.Updates = append(s.data.Updates, Update{Version: version, At: now.UnixMilli()})
	return true
}

func (s *Store) saveLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, raw, 0o600)
}

// Record is a copy of what the app reads.
func (s *Store) Record() Record {
	if s == nil {
		return Record{Seen: []string{}, Updates: []Update{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Record
	out.Seen = append([]string{}, s.data.Seen...)
	out.Updates = append([]Update{}, s.data.Updates...)
	return out
}

// MarkSeen adds highlight ids to what the person has seen and ends a replay.
func (s *Store) MarkSeen(ids []string) (Record, error) {
	s.mu.Lock()
	known := make(map[string]bool, len(s.data.Seen))
	for _, id := range s.data.Seen {
		known[id] = true
	}
	changed := s.data.Replay
	s.data.Replay = false
	for _, id := range ids {
		if id == "" || len(id) > 64 || known[id] || len(s.data.Seen) >= maxSeen {
			continue
		}
		known[id] = true
		s.data.Seen = append(s.data.Seen, id)
		changed = true
	}
	var err error
	if changed {
		err = s.saveLocked()
	}
	s.mu.Unlock()
	return s.Record(), err
}

// NoteUpdate records a version the app reached (its own update), once.
func (s *Store) NoteUpdate(version string, now time.Time) (Record, error) {
	if !validVersion(version) {
		return s.Record(), errors.New("not a version")
	}
	s.mu.Lock()
	var err error
	if s.addUpdateLocked(version, now) {
		err = s.saveLocked()
	}
	s.mu.Unlock()
	return s.Record(), err
}

// Replay makes every highlight news again and asks the app to show the
// sheet once whatever the versions say (`mewla dev whats-new`).
func (s *Store) Replay() (Record, error) {
	s.mu.Lock()
	s.data.Seen = nil
	s.data.Baseline = ""
	s.data.Replay = true
	err := s.saveLocked()
	s.mu.Unlock()
	return s.Record(), err
}
