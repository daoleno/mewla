//go:build windows

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// The lock covers one byte far past any content, so it never blocks reads
// or writes of the file itself.
const lockOffsetHigh = 0x7fffffff

func lockFile(file *os.File, flags uint32) error {
	overlapped := windows.Overlapped{OffsetHigh: lockOffsetHigh}
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
}

// TryLock takes an exclusive lock on file without waiting. ok is false when
// another process holds it.
func TryLock(file *os.File) (ok bool, err error) {
	err = lockFile(file, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

// Lock takes an exclusive lock on file, waiting for another holder.
func Lock(file *os.File) error { return lockFile(file, windows.LOCKFILE_EXCLUSIVE_LOCK) }

// Unlock releases a lock taken by TryLock or Lock.
func Unlock(file *os.File) error {
	overlapped := windows.Overlapped{OffsetHigh: lockOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
