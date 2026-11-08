//go:build !windows

package server

import (
	"os/exec"
	"syscall"
)

// gitOwnProcessGroup runs git (and the hooks and helpers it starts) in its own
// process group and, when the deadline passes, asks the whole group to stop
// with SIGTERM, so a hook or a pager is not left holding index.lock.
func gitOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
