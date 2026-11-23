//go:build linux

package gc

import (
	"io/fs"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// accessTime is when the file was last read, as the kernel records it.
func accessTime(info fs.FileInfo) time.Time {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Atim.Unix())
}

// recordsReads reports whether the file system at path moves access times on
// reads: false when it is mounted noatime, or cannot be asked.
func recordsReads(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	return st.Flags&unix.ST_NOATIME == 0
}
