//go:build windows

package sessionhost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/platform"
)

// On Windows the host's socket is an AF_UNIX socket in
// %LOCALAPPDATA%\Mewla\run, a folder whose ACL (inherited from the user's
// profile) lets only this user, SYSTEM and Administrators in. Each
// connection's peer process must also run as this user.

// otherUser is the peer uid reported for a process of another user. Windows
// has no uids: os.Getuid is -1, so this user's processes report that.
const otherUser = 0xfffffffe

// SocketPath is the host socket for a state directory:
// %LOCALAPPDATA%\Mewla\run\host-<8 hex of the state dir>.sock. It only
// computes the path.
func SocketPath(stateDir string) (string, error) {
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("session host socket: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
	base := os.Getenv("LOCALAPPDATA")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("session host socket: %w", err)
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "Mewla", "run", "host-"+hex.EncodeToString(digest[:4])+".sock"), nil
}

// EnsureSocketDir makes the socket's folder and requires it to be a real
// folder this user owns.
func EnsureSocketDir(socketPath string) error {
	if err := ensurePrivateDir(filepath.Dir(socketPath), os.Getuid()); err != nil {
		return fmt.Errorf("session host socket directory: %w", err)
	}
	return nil
}

func ensurePrivateDir(dir string, _ int) error {
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
	owned, err := platform.DirOwnedByCurrentUser(dir)
	if err != nil {
		return fmt.Errorf("%s: read its owner: %w", dir, err)
	}
	if !owned {
		return fmt.Errorf("%s belongs to another user", dir)
	}
	return nil
}

// listen binds the socket under an exclusive lock held for the host's life,
// so a second host on the same path fails instead of stealing it. A socket
// file without a host behind it is a leftover and is replaced.
func listen(path string) (*net.UnixListener, *os.File, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("session host lock: %w", err)
	}
	ok, err := platform.TryLock(lock)
	if err != nil || !ok {
		lock.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("session host lock: %w", err)
		}
		return nil, nil, fmt.Errorf("another session host is running on %s", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		return nil, nil, fmt.Errorf("remove leftover session host socket: %w", err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		lock.Close()
		return nil, nil, fmt.Errorf("session host socket: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	return listener, lock, nil
}

// peerUID stands in for the peer's uid: this user's (os.Getuid) when the
// process at the other end runs as this user, otherUser when it does not.
func peerUID(conn *net.UnixConn) (uint32, error) {
	pid, err := platform.UnixPeerPID(conn)
	if err != nil {
		return 0, err
	}
	same, err := platform.ProcessIsCurrentUser(pid)
	if err != nil {
		return 0, err
	}
	if !same {
		return otherUser, nil
	}
	return uint32(os.Getuid()), nil
}
