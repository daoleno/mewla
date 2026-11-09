//go:build windows

package platform

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SIO_AF_UNIX_GETPEERPID (_WSAIOR(IOC_VENDOR, 256)) returns the pid of the
// process on the other end of an AF_UNIX socket.
const sioAFUnixGetPeerPID = 0x58000100

// UnixPeerPID is the system's record of the process on the other end of
// conn.
func UnixPeerPID(conn *net.UnixConn) (int, error) {
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

// ProcessIsCurrentUser reports whether pid runs as the same user as this
// process (the user SID of its token). A process this user cannot open or
// query is someone else's.
func ProcessIsCurrentUser(pid int) (bool, error) {
	own, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			return false, nil
		}
		return false, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			return false, nil
		}
		return false, fmt.Errorf("open token of %d: %w", pid, err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return false, err
	}
	return windows.EqualSid(user.User.Sid, own.User.Sid), nil
}

// DirOwnedByCurrentUser reports whether this user owns dir (or the
// Administrators group does, for a folder made from an elevated window by
// this same user).
func DirOwnedByCurrentUser(dir string) (bool, error) {
	own, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, err
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false, err
	}
	if windows.EqualSid(owner, own.User.Sid) {
		return true, nil
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil || !windows.EqualSid(owner, admins) {
		return false, err
	}
	return windows.GetCurrentProcessToken().IsMember(admins)
}
