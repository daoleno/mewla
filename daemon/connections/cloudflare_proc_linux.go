//go:build linux

package connections

import "syscall"

// cf stops with the daemon instead of waiting on a sign-in nobody can see.
func cloudflareProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
