//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// ShortSocketDir is %LOCALAPPDATA%\Mewla\run, where the session host's socket
// already lives.
func ShortSocketDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" || !filepath.IsAbs(base) {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, "AppData", "Local")
		}
	}
	return filepath.Join(base, "Mewla", "run")
}

// ensureOwnedPrivateDir refuses a folder that is a link or another user's,
// then gives it the private ACL (MkdirPrivate) that Codex requires of the
// folder its control socket is in.
func ensureOwnedPrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("%s is not a plain folder", dir)
	}
	owned, err := DirOwnedByCurrentUser(dir)
	if err != nil {
		return fmt.Errorf("%s: read its owner: %w", dir, err)
	}
	if !owned {
		return fmt.Errorf("%s belongs to another user", dir)
	}
	return MkdirPrivate(dir)
}
