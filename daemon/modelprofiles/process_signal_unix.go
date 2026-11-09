//go:build !windows

package modelprofiles

import "syscall"

func signalProcess(pid int, force bool) {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(pid, sig)
}

func processExists(pid int) bool { return syscall.Kill(pid, 0) == nil }
