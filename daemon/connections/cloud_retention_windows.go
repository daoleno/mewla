//go:build windows

package connections

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
)

// Windows uses local vendor installs, but profile backup retention still needs
// cross-process exclusion around credential/config mutations.
func lockCloudFile(path string, shared, nonblocking bool) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var flags uint32
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if nonblocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	var overlapped windows.Overlapped
	if err = windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func inheritCloudLease(*exec.Cmd, *os.File) {}
