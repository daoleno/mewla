//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	accR = unix.R_OK
	accW = unix.W_OK
	accX = unix.X_OK
)

func accessPath(path string, mode uint32) error { return unix.Access(path, mode) }

func bootOwnerOfInfo(info os.FileInfo) bootProcessOwner {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return bootProcessOwnerUnknown
	}
	if int(stat.Uid) == os.Getuid() {
		return bootProcessOwnerSameUID
	}
	return bootProcessOwnerForeignUID
}

// bootExecutableMode reports whether a file has an execute bit.
func bootExecutableMode(info os.FileInfo) bool { return info.Mode().Perm()&0o111 != 0 }

// notifyTerminalResize sends on ch when this terminal changes size.
func notifyTerminalResize(ch chan<- os.Signal) { signal.Notify(ch, syscall.SIGWINCH) }
