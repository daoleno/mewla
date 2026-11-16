//go:build !windows

package control

import (
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// stagingTries bounds the names tried when one is taken.
const stagingTries = 8

// listenPublished listens on path so that the file appears only once it accepts
// connections, already private to its owner: it binds a short name of its own
// beside it, listens, and renames it into place. Bound in place, the file exists
// from bind, before listen, and a client that finds it then is refused ("Mewla
// is not running" while it starts). The name is random, so servers sharing a
// directory never take or remove each other's, and it is never longer than
// ".c" and six characters, so the path stays within the socket limit whenever
// path itself does with a name that long.
func listenPublished(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	for try := 1; ; try++ {
		staging := filepath.Join(dir, ".c"+strconv.FormatUint(rand.Uint64N(1<<30), 36))
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: staging, Net: "unix"})
		if errors.Is(err, syscall.EADDRINUSE) && try < stagingTries {
			continue
		}
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
}
