//go:build unix

package agentuse

import (
	"os"
	"syscall"
)

// lockedByAnotherProcess reports whether another process holds a POSIX
// record lock on path's byte range. It only asks; it never takes the lock.
func lockedByAnotherProcess(path string, start, length int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: start, Len: length}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lock); err != nil {
		return false
	}
	return lock.Type != syscall.F_UNLCK
}
