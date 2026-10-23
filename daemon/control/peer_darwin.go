//go:build darwin

package control

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerPID is the kernel's record of the process on the other end of conn
// (LOCAL_PEERPID).
func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var pidErr error
	if err := raw.Control(func(fd uintptr) {
		pid, pidErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, err
	}
	if pidErr != nil {
		return 0, fmt.Errorf("read peer pid: %w", pidErr)
	}
	return pid, nil
}
