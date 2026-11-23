package gc

import (
	"path/filepath"
	"sort"
	"strings"
)

// Usage is what running processes hold open on the file system: their
// executables, working directories and open files, read once (ReadUsage).
type Usage struct {
	paths []string // sorted, cleaned
	owner map[string]int
}

// Uses reports a process holding path or anything under it, and which.
func (u Usage) Uses(path string) (int, bool) {
	path = filepath.Clean(path)
	if i := sort.SearchStrings(u.paths, path); i < len(u.paths) && u.paths[i] == path {
		return u.owner[path], true
	}
	// "a/b-c" sorts between "a/b" and "a/b/…": look for the subtree itself.
	under := path + string(filepath.Separator)
	if i := sort.SearchStrings(u.paths, under); i < len(u.paths) && strings.HasPrefix(u.paths[i], under) {
		return u.owner[u.paths[i]], true
	}
	return 0, false
}

func newUsage(held map[string]int) Usage {
	u := Usage{owner: held}
	for path := range held {
		u.paths = append(u.paths, path)
	}
	sort.Strings(u.paths)
	return u
}
