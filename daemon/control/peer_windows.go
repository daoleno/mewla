//go:build windows

package control

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SIO_AF_UNIX_GETPEERPID (_WSAIOR(IOC_VENDOR, 256)) returns the pid of the
// process on the other end of an AF_UNIX socket.
const sioAFUnixGetPeerPID = 0x58000100

// peerPID is the system's record of the process on the other end of conn.
func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid uint32
	var ioctlErr error
	if err := raw.Control(func(fd uintptr) {
		var returned uint32
		ioctlErr = windows.WSAIoctl(windows.Handle(fd), sioAFUnixGetPeerPID, nil, 0,
			(*byte)(unsafe.Pointer(&pid)), uint32(unsafe.Sizeof(pid)), &returned, nil, 0)
	}); err != nil {
		return 0, err
	}
	if ioctlErr != nil {
		return 0, fmt.Errorf("read peer pid: %w", ioctlErr)
	}
	return int(pid), nil
}
