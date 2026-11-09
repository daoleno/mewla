//go:build windows

package control

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/daoleno/mewla/daemon/platform"
)

const lifecycleLockName = "daemon.lock"

// LifecycleLock proves exclusive ownership of one daemon state directory.
// On Windows it is a LockFileEx lock, which the system releases when the
// process ends, as flock is on Unix.
type LifecycleLock struct {
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

func TryAcquireLifecycleLock(stateDir string) (*LifecycleLock, bool, error) {
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		return nil, false, err
	}
	runDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create daemon runtime directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(runDir, lifecycleLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open daemon lifecycle lock: %w", err)
	}
	ok, err := platform.TryLock(file)
	if err != nil || !ok {
		_ = file.Close()
		if err != nil {
			return nil, false, fmt.Errorf("lock daemon lifecycle: %w", err)
		}
		return nil, false, nil
	}
	return &LifecycleLock{file: file}, true, nil
}

func (lock *LifecycleLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.closeOnce.Do(func() {
		if lock.file == nil {
			return
		}
		unlockErr := platform.Unlock(lock.file)
		closeErr := lock.file.Close()
		switch {
		case unlockErr != nil:
			lock.closeErr = fmt.Errorf("unlock daemon lifecycle: %w", unlockErr)
		case closeErr != nil:
			lock.closeErr = fmt.Errorf("close daemon lifecycle lock: %w", closeErr)
		}
	})
	return lock.closeErr
}
