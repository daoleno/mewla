//go:build !linux

package connections

import "syscall"

// Elsewhere an orphaned sign-in ends on its own when cf's code expires.
func cloudflareProcAttr() *syscall.SysProcAttr { return nil }
