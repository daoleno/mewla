//go:build linux || darwin

package sessionhost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// SocketPath is the host socket for a state directory:
// <runtime>/host-<8 hex of the state dir>.sock, where <runtime> is
// $XDG_RUNTIME_DIR/mewla on Linux and $TMPDIR/mewla on macOS when those
// belong to this user, and <tmp>/mewla-<uid> otherwise. The directory is
// created 0700 and must belong to this user; a directory anyone else could
// have prepared is refused, as tmux does.
func SocketPath(stateDir string) (string, error) {
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("session host socket: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
	dir, err := socketDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "host-"+hex.EncodeToString(digest[:4])+".sock"), nil
}

func socketDir() (string, error) {
	uid := os.Getuid()
	var base string
	switch runtime.GOOS {
	case "linux":
		base = os.Getenv("XDG_RUNTIME_DIR")
	case "darwin":
		base = os.Getenv("TMPDIR")
	}
	if base != "" && filepath.IsAbs(base) && ownedPrivateDir(base, uid) == nil {
		dir := filepath.Join(base, "mewla")
		if err := ensurePrivateDir(dir, uid); err == nil {
			return dir, nil
		}
	}
	dir := filepath.Join("/tmp", fmt.Sprintf("mewla-%d", uid))
	if err := ensurePrivateDir(dir, uid); err != nil {
		return "", fmt.Errorf("session host socket directory: %w", err)
	}
	return dir, nil
}

func ensurePrivateDir(dir string, uid int) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return ownedPrivateDir(dir, uid)
}

// ownedPrivateDir requires a real directory (not a symlink) owned by uid that
// no one else can read, write or enter.
func ownedPrivateDir(dir string, uid int) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if int(stat.Uid) != uid {
		return fmt.Errorf("%s belongs to uid %d, not %d", dir, stat.Uid, uid)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is open to other users (mode %v)", dir, info.Mode().Perm())
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
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, nil, fmt.Errorf("another session host is running on %s", path)
		}
		return nil, nil, fmt.Errorf("session host lock: %w", err)
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
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		lock.Close()
		return nil, nil, fmt.Errorf("session host socket: %w", err)
	}
	return listener, lock, nil
}

// peerUID is the uid of the process at the other end of a Unix socket.
func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var credErr error
	if err := raw.Control(func(fd uintptr) { uid, credErr = socketPeerUID(int(fd)) }); err != nil {
		return 0, err
	}
	return uid, credErr
}
