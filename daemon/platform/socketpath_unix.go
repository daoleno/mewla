//go:build unix

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ShortSocketDir is /tmp/mewla-<uid>. It is the same for every process of
// this user whatever its environment ($TMPDIR on a Mac is itself long, and a
// child's $XDG_RUNTIME_DIR may differ from the daemon's).
func ShortSocketDir() string {
	return filepath.Join("/tmp", fmt.Sprintf("mewla-%d", os.Getuid()))
}

func ensureOwnedPrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to uid %d, not %d", dir, stat.Uid, os.Getuid())
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is open to other users (mode %v)", dir, info.Mode().Perm())
	}
	return nil
}
