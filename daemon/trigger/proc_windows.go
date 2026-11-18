//go:build windows

package trigger

import (
	"os/exec"
	"syscall"
)

// isolate gives the check no console window; a timeout kills the shell.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
