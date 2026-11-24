package gc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A keep is a Session's word that a path in its scratch must outlive it:
// until a date, for a reason. When the Session ends the path moves out of the
// scratch to KeptPath; when the keep ends it goes to the trash. A keep can
// also give a term to a path in no Session's scratch (Brain's backup in t/):
// the collector leaves it alone until the date, then trashes it where it is.
const (
	KindKept = "kept"
	// KeepLimit is the longest a Session may keep something.
	KeepLimit = 30 * 24 * time.Hour

	ReasonKeepEnded = "its keep ended"
	ReasonReleased  = "released by its Session"
)

// Keep is one keep record (gc/keep/<id>.json).
type Keep struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`  // where the Session has it
	Owner     string    `json:"owner"` // the Session's resource unit
	Until     time.Time `json:"until"`
	Why       string    `json:"why"`
	CreatedAt time.Time `json:"created_at"`
	// Moved is where it went when its Session ended ("" until then).
	Moved string `json:"moved,omitempty"`
}

func keepRoot(stateRoot string) string { return filepath.Join(stateRoot, "gc", "keep") }

// KeptPath is where a kept path goes when its Session ends.
func KeptPath(stateRoot string, k Keep) string {
	return filepath.Join(stateRoot, "gc", "kept", k.ID, filepath.Base(k.Path))
}

// AddKeep records that owner keeps path until until, for why. The caller has
// checked that path is inside owner's scratch.
func AddKeep(stateRoot, path, owner string, until time.Time, why string, now time.Time) (Keep, error) {
	why = strings.TrimSpace(why)
	switch {
	case why == "":
		return Keep{}, errors.New("a keep needs a reason (--why)")
	case !until.After(now):
		return Keep{}, fmt.Errorf("--until %s is not in the future", until.Format(time.DateOnly))
	case until.Sub(now) > KeepLimit:
		return Keep{}, fmt.Errorf("--until %s is more than %d days away; ask Brain for longer", until.Format(time.DateOnly), int(KeepLimit.Hours()/24))
	}
	keeps, err := ListKeeps(stateRoot)
	if err != nil {
		return Keep{}, err
	}
	for _, k := range keeps {
		if k.Owner == owner && k.Path == path {
			k.Until, k.Why = until.UTC(), why
			if err := writeKeep(stateRoot, k); err != nil {
				return k, err
			}
			Record(stateRoot, Event{At: now, Action: ActionKept, Kind: KindKept, Subject: path, Owner: owner, Detail: keepDetail(k)})
			return k, nil
		}
	}
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	k := Keep{
		ID:   now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(raw[:]),
		Path: path, Owner: owner, Until: until.UTC(), Why: why, CreatedAt: now.UTC(),
	}
	if err := writeKeep(stateRoot, k); err != nil {
		return k, err
	}
	Record(stateRoot, Event{At: now, Action: ActionKept, Kind: KindKept, Subject: path, Owner: owner, Detail: keepDetail(k)})
	return k, nil
}

func keepDetail(k Keep) string {
	return k.ID + ": until " + k.Until.Format(time.DateOnly) + ": " + k.Why
}

// ListKeeps returns every keep, oldest first.
func ListKeeps(stateRoot string) ([]Keep, error) {
	paths, err := filepath.Glob(filepath.Join(keepRoot(stateRoot), "*.json"))
	if err != nil {
		return nil, err
	}
	var keeps []Keep
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var k Keep
		if json.Unmarshal(raw, &k) == nil && k.ID != "" {
			keeps = append(keeps, k)
		}
	}
	sort.Slice(keeps, func(a, b int) bool { return keeps[a].ID < keeps[b].ID })
	return keeps, nil
}

func writeKeep(stateRoot string, k Keep) error {
	if err := os.MkdirAll(keepRoot(stateRoot), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(keepRoot(stateRoot), k.ID+".json"), append(data, '\n'))
}

func removeKeep(stateRoot, id string) error {
	err := os.Remove(filepath.Join(keepRoot(stateRoot), id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// KeepNote says what happened to one keep when its Session ended.
type KeepNote struct {
	Keep Keep
	Gone bool // the path was no longer there: the keep is dropped
}

// MoveKept moves owner's kept paths out of its scratch, which is about to go:
// each to KeptPath. The record is written before the move, so a crash between
// them leaves a keep whose move is finished next time.
func MoveKept(stateRoot, owner, scratch string) ([]KeepNote, error) {
	keeps, err := ListKeeps(stateRoot)
	if err != nil {
		return nil, err
	}
	var notes []KeepNote
	for _, k := range keeps {
		if k.Owner != owner || !within(k.Path, scratch) {
			continue
		}
		dest := KeptPath(stateRoot, k)
		if k.Moved != "" {
			if _, err := os.Lstat(dest); err == nil {
				continue // moved already
			}
		}
		if _, err := os.Lstat(k.Path); err != nil {
			if _, done := os.Lstat(dest); done == nil {
				continue
			}
			if err := removeKeep(stateRoot, k.ID); err != nil {
				return notes, err
			}
			notes = append(notes, KeepNote{Keep: k, Gone: true})
			continue
		}
		k.Moved = dest
		if err := writeKeep(stateRoot, k); err != nil {
			return notes, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return notes, err
		}
		if err := os.Rename(k.Path, dest); err != nil {
			return notes, fmt.Errorf("move kept %s: %w", k.Path, err)
		}
		Record(stateRoot, Event{Action: ActionKeptMoved, Kind: KindKept, Subject: k.Path, Owner: k.Owner, Bytes: Size(dest), Detail: k.ID + ": to " + dest})
		notes = append(notes, KeepNote{Keep: k})
	}
	return notes, nil
}

// ExpireKeeps ends the keeps past their date: a moved path goes to the trash;
// one still in a live Session's scratch simply stops being kept.
func ExpireKeeps(stateRoot string, now time.Time) ([]Keep, error) {
	keeps, err := ListKeeps(stateRoot)
	if err != nil {
		return nil, err
	}
	var ended []Keep
	for _, k := range keeps {
		if now.Before(k.Until) {
			continue
		}
		if err := dropKeep(stateRoot, k, ReasonKeepEnded, now); err != nil {
			return ended, err
		}
		ended = append(ended, k)
	}
	return ended, nil
}

// ReleaseKeep ends owner's keep id now (gc release): a path already moved
// out of its Session goes to the trash.
func ReleaseKeep(stateRoot, id, owner string, now time.Time) (Keep, error) {
	keeps, err := ListKeeps(stateRoot)
	if err != nil {
		return Keep{}, err
	}
	for _, k := range keeps {
		if k.ID != id {
			continue
		}
		if k.Owner != owner {
			return Keep{}, fmt.Errorf("keep %s is another Session's", id)
		}
		return k, dropKeep(stateRoot, k, ReasonReleased, now)
	}
	return Keep{}, fmt.Errorf("no keep %s", id)
}

func dropKeep(stateRoot string, k Keep, reason string, now time.Time) error {
	Record(stateRoot, Event{At: now, Action: ActionKeepEnded, Kind: KindKept, Subject: k.Path, Owner: k.Owner, Detail: k.ID + ": " + reason})
	switch {
	case k.Moved != "":
		if _, err := os.Lstat(k.Moved); err == nil {
			if _, err := Trash(stateRoot, KindKept, k.Moved, k.Owner, reason, now); err != nil {
				return err
			}
		}
		_ = os.Remove(filepath.Dir(k.Moved))
	case !inMarkedScratch(stateRoot, k.Path):
		// No Session's scratch takes it away: the keep was its only term.
		if _, err := os.Lstat(k.Path); err == nil {
			if _, err := Trash(stateRoot, KindKept, k.Path, k.Owner, reason, now); err != nil {
				return err
			}
		}
	}
	return removeKeep(stateRoot, k.ID)
}

// inMarkedScratch reports whether path is in a Session's scratch, which its
// lease release takes to the trash.
func inMarkedScratch(stateRoot, path string) bool {
	scratch, ok := scratchOf(stateRoot, path)
	return ok && hasMarker(scratch)
}

func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
