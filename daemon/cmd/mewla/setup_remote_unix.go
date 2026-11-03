//go:build unix

package main

import "syscall"

// detachedSysProcAttr puts a child in its own session, so closing the SSH
// connection that started it does not end it.
func detachedSysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
