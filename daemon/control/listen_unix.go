//go:build !windows

package control

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// listenPublished listens on path so that the file appears only once it accepts
// connections, already private to its owner: it binds a short name beside it,
// listens, and renames it into place. Bound in place, the file exists from bind,
// before listen, and a client that finds it then is refused ("Mewla is not
// running" while it starts).
func listenPublished(path string) (net.Listener, error) {
	staging := filepath.Join(filepath.Dir(path), fmt.Sprintf(".c%d", os.Getpid()))
	_ = os.Remove(staging)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: staging, Net: "unix"})
	if err != nil {
		return nil, err
	}
	// The socket lives on under path; Run removes that when it stops.
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(staging, 0o600); err != nil {
		listener.Close()
		_ = os.Remove(staging)
		return nil, err
	}
	if err := os.Rename(staging, path); err != nil {
		listener.Close()
		_ = os.Remove(staging)
		return nil, err
	}
	return listener, nil
}
