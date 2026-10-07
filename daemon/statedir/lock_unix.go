//go:build linux || darwin

package statedir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var errExchangeUnsupported = errors.New("atomic exchange unsupported")

// lockDaemons takes every existing daemon lifecycle lock below root without
// blocking. busy names the first lock a live daemon holds. Locks are not
// created: a root that never ran a daemon has nothing to hold.
func lockDaemons(root string) ([]*os.File, string, error) {
	var held []*os.File
	for _, rel := range DaemonLockPaths {
		path := filepath.Join(root, rel)
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return held, "", fmt.Errorf("open daemon lock %s: %w", path, err)
		}
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			_ = file.Close()
			if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
				return held, path, nil
			}
			return held, "", fmt.Errorf("lock %s: %w", path, err)
		}
		held = append(held, file)
	}
	return held, "", nil
}

func releaseLocks(files []*os.File) {
	for _, file := range files {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}
}

func isUnsupportedMove(err error) bool {
	return errors.Is(err, unix.EXDEV) || errors.Is(err, unix.EBUSY)
}
