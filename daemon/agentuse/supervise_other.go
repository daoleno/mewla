//go:build !unix

package agentuse

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr { return nil }
