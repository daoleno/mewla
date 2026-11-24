// Package gc is Mewla's collector of what its Sessions leave behind. This
// first piece is the trash: what a release collects is moved here, not
// deleted, and is deleted only after TrashGrace, so a wrong collection can be
// put back with Restore.
//
// Layout, under the state root:
//
//	gc/trash/<YYYY-MM-DD>/<id>       the moved directory or file
//	gc/trash/<YYYY-MM-DD>/<id>.json  its record (written before the move)
//
// The record is written before the move, so a crash between the two leaves a
// record whose item is still at its original path; List shows it as not
// moved and Finalize drops it. The trash lives under the state root so a
// move from the scratch root (also under it) is one rename on one filesystem.
package gc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// TrashGrace is how long a trashed item can be restored before Finalize
// deletes it.
const TrashGrace = 72 * time.Hour

// KindScratch is a Session's scratch directory (its TMPDIR).
const KindScratch = "scratch"

// Item is one thing in the trash.
type Item struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Original  string    `json:"original"`
	Owner     string    `json:"owner,omitempty"`  // the resource unit it belonged to
	Reason    string    `json:"reason,omitempty"` // why it was collected
	TrashedAt time.Time `json:"trashed_at"`
	// Path is where the item is now; set by List, never stored.
	Path string `json:"-"`
	// Moved says the item is in the trash (false: the move never happened).
	Moved bool `json:"-"`
}

// DeleteAfter is when Finalize deletes the item.
func (i Item) DeleteAfter() time.Time { return i.TrashedAt.Add(TrashGrace) }

// TrashRoot is the trash directory under a state root.
func TrashRoot(stateRoot string) string { return filepath.Join(stateRoot, "gc", "trash") }

var (
	idRE       = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[A-Za-z0-9_-]{1,40}$`)
	dateDirRE  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	idSanitize = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
)

// Trash moves original into the trash and returns its record. original must
// be a real directory or file (never a symlink: the link would be moved, not
// what it names, which is fine, but a symlinked root is refused so a swapped
// path cannot redirect a move).
func Trash(stateRoot, kind, original, owner, reason string, now time.Time) (Item, error) {
	original = filepath.Clean(original)
	info, err := os.Lstat(original)
	if err != nil {
		return Item{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Item{}, fmt.Errorf("refuse to trash symlink %s", original)
	}
	now = now.UTC()
	day := filepath.Join(TrashRoot(stateRoot), now.Format("2006-01-02"))
	if err := os.MkdirAll(day, 0o700); err != nil {
		return Item{}, fmt.Errorf("create trash: %w", err)
	}
	base := idSanitize.ReplaceAllString(filepath.Base(original), "")
	if base == "" {
		base = kind
	}
	if len(base) > 32 {
		base = base[:32]
	}
	stamp := now.Format("20060102-150405")
	var id string
	for n := 0; ; n++ {
		id = stamp + "-" + base
		if n > 0 {
			id = fmt.Sprintf("%s-%s-%d", stamp, base, n)
		}
		if _, err := os.Lstat(filepath.Join(day, id+".json")); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if n > 100 {
			return Item{}, fmt.Errorf("no free trash id for %s", original)
		}
	}
	item := Item{ID: id, Kind: kind, Original: original, Owner: owner, Reason: reason, TrashedAt: now}
	raw, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return Item{}, err
	}
	record := filepath.Join(day, id+".json")
	if err := atomicfile.Write(record, append(raw, '\n'), 0o600); err != nil {
		return Item{}, fmt.Errorf("write trash record: %w", err)
	}
	target := filepath.Join(day, id)
	if err := os.Rename(original, target); err != nil {
		_ = os.Remove(record)
		return Item{}, fmt.Errorf("move %s to the trash: %w", original, err)
	}
	item.Path, item.Moved = target, true
	Record(stateRoot, Event{At: now, Action: ActionTrashed, Kind: kind, Subject: original, Owner: owner, Bytes: Size(target), Detail: id + ": " + reason})
	return item, nil
}

// List returns every record in the trash, oldest first.
func List(stateRoot string) ([]Item, error) {
	root := TrashRoot(stateRoot)
	days, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, day := range days {
		if !day.IsDir() || !dateDirRE.MatchString(day.Name()) {
			continue
		}
		dir := filepath.Join(root, day.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".json") || !idRE.MatchString(strings.TrimSuffix(name, ".json")) {
				continue
			}
			item, err := readRecord(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			item.Path = filepath.Join(dir, item.ID)
			if _, err := os.Lstat(item.Path); err == nil {
				item.Moved = true
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(a, b int) bool {
		if !items[a].TrashedAt.Equal(items[b].TrashedAt) {
			return items[a].TrashedAt.Before(items[b].TrashedAt)
		}
		return items[a].ID < items[b].ID
	})
	return items, nil
}

func readRecord(path string) (Item, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Item{}, err
	}
	var item Item
	if err := json.Unmarshal(raw, &item); err != nil {
		return Item{}, err
	}
	if !idRE.MatchString(item.ID) || filepath.Base(path) != item.ID+".json" || item.Original == "" {
		return Item{}, fmt.Errorf("invalid trash record %s", path)
	}
	return item, nil
}

func find(stateRoot, id string) (Item, error) {
	if !idRE.MatchString(id) {
		return Item{}, fmt.Errorf("no trashed item %q", id)
	}
	items, err := List(stateRoot)
	if err != nil {
		return Item{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return Item{}, fmt.Errorf("no trashed item %q", id)
}

// Restore puts an item back at its original path and drops its record. It
// refuses when something is already at that path.
func Restore(stateRoot, id string) (Item, error) {
	item, err := find(stateRoot, id)
	if err != nil {
		return Item{}, err
	}
	if !item.Moved {
		return Item{}, fmt.Errorf("%s was never moved to the trash; it is still at %s", id, item.Original)
	}
	if _, err := os.Lstat(item.Original); err == nil {
		return Item{}, fmt.Errorf("%s already exists", item.Original)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Item{}, err
	}
	if err := os.MkdirAll(filepath.Dir(item.Original), 0o700); err != nil {
		return Item{}, err
	}
	if err := os.Rename(item.Path, item.Original); err != nil {
		return Item{}, fmt.Errorf("restore %s: %w", id, err)
	}
	_ = os.Remove(item.Path + ".json")
	Record(stateRoot, Event{Action: ActionRestored, Kind: item.Kind, Subject: item.Original, Owner: item.Owner, Detail: id})
	return item, nil
}

// Finalize deletes every item trashed more than TrashGrace before now, and
// drops records whose item was never moved. It returns what it deleted.
func Finalize(stateRoot string, now time.Time) ([]Item, error) {
	items, err := List(stateRoot)
	if err != nil {
		return nil, err
	}
	var deleted []Item
	var errs []error
	for _, item := range items {
		record := item.Path + ".json"
		if !item.Moved {
			if now.Sub(item.TrashedAt) > time.Hour {
				_ = os.Remove(record)
			}
			continue
		}
		if now.Before(item.DeleteAfter()) {
			continue
		}
		bytes := Size(item.Path)
		if err := RemoveTree(item.Path); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", item.ID, err))
			continue
		}
		_ = os.Remove(record)
		Record(stateRoot, Event{At: now, Action: ActionDeleted, Kind: item.Kind, Subject: item.Original, Owner: item.Owner, Bytes: bytes, Detail: item.ID})
		deleted = append(deleted, item)
	}
	removeEmptyDays(TrashRoot(stateRoot))
	return deleted, errors.Join(errs...)
}

func removeEmptyDays(root string) {
	days, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, day := range days {
		if day.IsDir() && dateDirRE.MatchString(day.Name()) {
			_ = os.Remove(filepath.Join(root, day.Name())) // only succeeds when empty
		}
	}
}

// Size is the bytes under path (files only; symlinks are not followed).
func Size(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// RemoveTree deletes a tree the caller has already validated as its own.
// Nested tool caches (for example Go module dirs) may be mode 0555/000 with
// 0444 files; owner rwx is restored on directories first so WalkDir can
// traverse and RemoveAll can unlink them. Symlinks are never followed.
func RemoveTree(root string) error {
	if err := MakeDirsOwnerAccessible(root); err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// MakeDirsOwnerAccessible walks root without following symlinks and adds
// owner read+write+execute only to real directories inside that tree.
func MakeDirsOwnerAccessible(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 || !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode&0o700 == 0o700 {
			return nil
		}
		return os.Chmod(path, mode|0o700)
	})
}
