//go:build linux || darwin

package connections

import (
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
)

func lockCloudFile(path string, shared, nonblocking bool) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	if nonblocking {
		mode |= unix.LOCK_NB
	}
	if err = unix.Flock(int(file.Fd()), mode); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func inheritCloudLease(cmd *exec.Cmd, lease *os.File) {
	if lease != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, lease)
	}
}
