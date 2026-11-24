package gc

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CacheBudget is what the shared cache may hold before the least recently
// changed entries are deleted.
const CacheBudget int64 = 64 << 30

// CacheDir is MEWLA_CACHE_DIR: what must outlive a Session (a build cache,
// a download) and may be rebuilt. It is not swept by Session; over the
// budget its top-level entries go, least recently changed first.
func CacheDir(stateRoot string) string { return filepath.Join(stateRoot, "gc", "cache") }

// Evicted is a cache entry deleted to bring the cache under its budget.
type Evicted struct {
	Path    string
	Bytes   int64
	Changed time.Time
}

// EvictCache deletes top-level entries of the cache, least recently changed
// first and never one a process uses, until it holds at most budget bytes.
// Cache entries are rebuilt, not restored: they are deleted, not trashed.
func EvictCache(stateRoot string, budget int64, usage Usage) ([]Evicted, error) {
	dir := CacheDir(stateRoot)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type entry struct {
		path    string
		bytes   int64
		changed time.Time
	}
	var all []entry
	var total int64
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		bytes, changed := treeSizeAndChange(path)
		all = append(all, entry{path, bytes, changed})
		total += bytes
	}
	if total <= budget {
		return nil, nil
	}
	sort.Slice(all, func(a, b int) bool { return all[a].changed.Before(all[b].changed) })
	var evicted []Evicted
	for _, e := range all {
		if total <= budget {
			break
		}
		if _, used := usage.Uses(e.path); used {
			continue
		}
		if err := RemoveTree(e.path); err != nil {
			return evicted, err
		}
		total -= e.bytes
		Record(stateRoot, Event{Action: ActionEvicted, Kind: SizeCache, Subject: e.path, Bytes: e.bytes, Detail: "cache over its budget; last changed " + e.changed.Format(time.RFC3339)})
		evicted = append(evicted, Evicted{Path: e.path, Bytes: e.bytes, Changed: e.changed})
	}
	return evicted, nil
}

func treeSizeAndChange(path string) (int64, time.Time) {
	var bytes int64
	var changed time.Time
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		if info.ModTime().After(changed) {
			changed = info.ModTime()
		}
		return nil
	})
	return bytes, changed
}
