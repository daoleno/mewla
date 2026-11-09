//go:build windows

package sqlitedb

import (
	"os"

	"github.com/daoleno/mewla/daemon/platform"
)

// lockFile takes an exclusive lock on path, waiting for another process
// that is migrating the same state directory, and returns the unlock.
func lockFile(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := platform.Lock(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = platform.Unlock(file)
		_ = file.Close()
	}, nil
}
