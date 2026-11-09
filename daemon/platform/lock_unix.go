//go:build unix

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes an exclusive lock on file without waiting. ok is false when
// another process holds it.
func TryLock(file *os.File) (ok bool, err error) {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Lock takes an exclusive lock on file, waiting for another holder.
func Lock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX) }

// Unlock releases a lock taken by TryLock or Lock.
func Unlock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }
