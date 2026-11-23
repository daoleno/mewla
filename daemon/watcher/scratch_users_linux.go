//go:build linux

package watcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// scratchUsage is a process whose executable or working directory is inside a
// scratch directory.
type scratchUsage struct {
	PID int
	Exe string
}

// scratchUser returns a running process whose exe or cwd is inside dir. It
// reads /proc; processes it cannot read (another user's) are skipped.
func scratchUser(dir string) (scratchUsage, bool) {
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return scratchUsage{}, false
	}
	self := os.Getpid()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		exe := procLinkPath(filepath.Join(base, "exe"))
		if exe != "" && pathWithinRoot(exe, dir) {
			return scratchUsage{PID: pid, Exe: exe}, true
		}
		if cwd := procLinkPath(filepath.Join(base, "cwd")); cwd != "" && pathWithinRoot(cwd, dir) {
			if exe == "" {
				exe = "?"
			}
			return scratchUsage{PID: pid, Exe: exe}, true
		}
	}
	return scratchUsage{}, false
}

// procLinkPath reads a /proc link, without the " (deleted)" a removed target
// carries.
func procLinkPath(link string) string {
	target, err := os.Readlink(link)
	if err != nil || !filepath.IsAbs(target) {
		return ""
	}
	return strings.TrimSuffix(target, " (deleted)")
}
