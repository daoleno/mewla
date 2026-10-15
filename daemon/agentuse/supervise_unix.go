//go:build unix

package agentuse

import "syscall"

// detachedProcAttr starts a process in a session of its own, out of reach of
// the signals meant for the agent's terminal and its MCP host.
func detachedProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
