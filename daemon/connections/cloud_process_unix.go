//go:build !windows

package connections

import (
	"os"
	"os/exec"
	"syscall"
)

// Each command owns its process group, including credential_process/installer
// children. Cancellation cannot leave those children detached from the flow.
func ownCloudProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cloudParentDeath(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
