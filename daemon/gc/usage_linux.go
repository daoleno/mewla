//go:build linux

package gc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadUsage reads every readable process's exe, cwd and open files from
// /proc. ok is false when /proc cannot be read: nothing can be told then.
func ReadUsage() (Usage, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Usage{}, false
	}
	self := os.Getpid()
	held := map[string]int{}
	add := func(pid int, link string) {
		target, err := os.Readlink(link)
		if err != nil || !filepath.IsAbs(target) {
			return
		}
		held[filepath.Clean(strings.TrimSuffix(target, " (deleted)"))] = pid
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		add(pid, filepath.Join(base, "exe"))
		add(pid, filepath.Join(base, "cwd"))
		fds, err := os.ReadDir(filepath.Join(base, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			add(pid, filepath.Join(base, "fd", fd.Name()))
		}
	}
	return newUsage(held), true
}
