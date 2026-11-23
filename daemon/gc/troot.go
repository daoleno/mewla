package gc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The t/ root is the runtime's: each Session's scratch is a marked t/<digest>
// there. Anything else that appears in it (an agent's mkdir next to its
// TMPDIR) belongs to no one, so once nothing uses it and nothing in it has
// changed for UnmarkedGrace it goes to the trash. What was there before this
// rule came is listed once and left alone.
const (
	KindUnmarked  = "unmarked"
	UnmarkedGrace = 48 * time.Hour

	scratchMarker = ".mewla-worker-unit"
)

// ReasonUnmarked is the trash reason of an unmarked t/ item.
const ReasonUnmarked = "unmarked in t/; unused and unchanged for 48 h"

type rootSnapshot struct {
	Since time.Time `json:"since"`
	Names []string  `json:"names"`
}

func snapshotPath(stateRoot string) string {
	return filepath.Join(stateRoot, "gc", "t-root-before.json")
}

// SweepUnmarked moves to the trash every unmarked item of the t/ root that
// was not there when the rule came (the first sweep lists those), that no
// process uses (usage) and in which nothing changed for UnmarkedGrace. Only
// directories and regular files are candidates; sockets, pipes, symlinks and
// *.lock files stay.
func SweepUnmarked(stateRoot string, usage Usage, now time.Time) ([]Item, error) {
	root := filepath.Join(stateRoot, "t")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	before, err := readSnapshot(stateRoot, entries, now)
	if err != nil {
		return nil, err
	}
	var moved []Item
	for _, entry := range entries {
		name := entry.Name()
		if before[name] {
			continue
		}
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil || !(info.IsDir() || info.Mode().IsRegular()) {
			continue
		}
		// A lock file is its inode: moving one a process is about to flock
		// would give two holders (client/scripts/phone.sh keeps one here).
		if info.Mode().IsRegular() && strings.HasSuffix(name, ".lock") {
			continue
		}
		if info.IsDir() {
			if marker, err := os.Lstat(filepath.Join(path, scratchMarker)); err == nil && marker.Mode().IsRegular() {
				continue // a Session's scratch: its lease releases it
			}
		}
		if _, used := usage.Uses(path); used {
			continue
		}
		if changedSince(path, now.Add(-UnmarkedGrace)) {
			continue
		}
		item, err := Trash(stateRoot, KindUnmarked, path, "", ReasonUnmarked, now)
		if err != nil {
			return moved, err
		}
		moved = append(moved, item)
	}
	return moved, nil
}

// readSnapshot returns the names the t/ root held when the rule came,
// writing them the first time.
func readSnapshot(stateRoot string, entries []os.DirEntry, now time.Time) (map[string]bool, error) {
	path := snapshotPath(stateRoot)
	var snapshot rootSnapshot
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
		snapshot.Since = now.UTC()
		for _, entry := range entries {
			snapshot.Names = append(snapshot.Names, entry.Name())
		}
		sort.Strings(snapshot.Names)
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(path, append(data, '\n')); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	names := make(map[string]bool, len(snapshot.Names))
	for _, name := range snapshot.Names {
		names[name] = true
	}
	return names, nil
}

// changedSince reports anything in the tree at path (itself included)
// modified after cutoff. It stops at the first one.
func changedSince(path string, cutoff time.Time) bool {
	found := false
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			found = true // unreadable: not proven unchanged
			return fs.SkipAll
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ScratchName is the name of a Session's scratch in the t/ root: six
// characters of its resource unit's digest, short enough to keep AF_UNIX
// paths inside it under the sun_path limit.
func ScratchName(unit string) string {
	sum := sha256.Sum256([]byte(unit))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:6]
}

// ScratchMarker is the file that names a scratch's resource unit.
const ScratchMarker = scratchMarker
