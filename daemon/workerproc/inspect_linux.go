package workerproc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Environ returns pid's environment, or nil when it cannot be read.
func Environ(pid int) []string {
	if pid <= 0 {
		return nil
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil
	}
	return strings.Split(string(raw), "\x00")
}

// Descendants returns every process below root, breadth first.
func Descendants(root int) []int {
	return descendants(root, childPIDs)
}

func childPIDs(pid int) []int {
	raw, err := os.ReadFile(filepath.Join(
		"/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children",
	))
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(raw))
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		child, err := strconv.Atoi(field)
		if err != nil || child <= 0 {
			continue
		}
		out = append(out, child)
	}
	return out
}

// OpenFiles returns the paths of pid's open file descriptors, or nil when they
// cannot be read.
func OpenFiles(pid int) []string {
	if pid <= 0 {
		return nil
	}
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if path, err := os.Readlink(filepath.Join(dir, entry.Name())); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}
