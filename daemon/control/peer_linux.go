//go:build linux

package control

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerPID is the kernel's record of the process on the other end of conn: the
// one that called listen on a socket we connected to.
func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credentials *unix.Ucred
	var credentialsErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialsErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credentialsErr != nil {
		return 0, fmt.Errorf("read peer credentials: %w", credentialsErr)
	}
	return int(credentials.Pid), nil
}
