//go:build linux || darwin

package brain

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockMigration takes an exclusive lock on path, waiting for another process
// that is migrating the same state directory, and returns the unlock.
func lockMigration(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
