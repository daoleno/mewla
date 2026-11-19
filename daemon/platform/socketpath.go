package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// socketPathLimit is the room a unix socket's address has for its path, the
// terminator included: 104 bytes on a Mac, 108 on Linux and Windows.
func socketPathLimit() int {
	if runtime.GOOS == "darwin" {
		return 104
	}
	return 108
}

// SocketPath is where a unix socket meant for path can be bound. A path that
// fits stays as it is. A longer one (a state directory under a long name)
// becomes a short name in this user's private socket directory (see
// ShortSocketDir), derived from path alone, so every process asking for the
// same path, listener or dialer, gets the same answer and no two paths share
// one. It only computes the path: EnsureSocketDir makes the directory.
func SocketPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if len(path) < socketPathLimit() {
		return path
	}
	sum := sha256.Sum256([]byte(path))
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if len(stem) > 16 {
		stem = stem[:16]
	}
	return filepath.Join(ShortSocketDir(), stem+"-"+hex.EncodeToString(sum[:8])+".sock")
}

// EnsureSocketDir makes the private socket directory when SocketPath put
// socket there. That directory must be this user's own, real and closed to
// everyone else; one somebody else prepared is refused. A socket that kept its
// own path is left to whoever owns its directory.
func EnsureSocketDir(socket string) error {
	dir := filepath.Dir(socket)
	if dir != ShortSocketDir() {
		return nil
	}
	if err := ensureOwnedPrivateDir(dir); err != nil {
		return fmt.Errorf("socket directory: %w", err)
	}
	return nil
}
