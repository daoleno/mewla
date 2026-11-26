//go:build unix

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// redirectFD makes fd a duplicate of file (dup2: no close-on-exec, so an exec
// in place keeps it).
func redirectFD(file *os.File, fd int) error {
	return unix.Dup2(int(file.Fd()), fd)
}
