//go:build !windows

package trigger

import (
	"os/exec"
	"syscall"
)

// isolate puts the check in its own process group, so a timeout kills
// everything it started, not only the shell.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
