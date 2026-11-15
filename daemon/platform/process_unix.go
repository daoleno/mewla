//go:build unix

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// StartDetached starts cmd in its own session, so it outlives the caller and
// its terminal.
func StartDetached(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	return cmd.Start()
}

// StartBackground is StartDetached: only Windows tells the two apart.
func StartBackground(cmd *exec.Cmd) error { return StartDetached(cmd) }

// ExecReplace replaces this process with path: the same PID runs the new
// image.
func ExecReplace(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}

// ProcessAlive reports whether pid exists.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// ProcessImagePath is the executable pid runs. On Linux a file replaced since
// the process started reads "<path> (deleted)"; without /proc (macOS) it is
// an error.
func ProcessImagePath(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
}

// ParentImage is Windows only: Unix has no parent image to check.
func ParentImage(pid int) (string, error) {
	return "", fmt.Errorf("parent image of %d: not on this system", pid)
}
