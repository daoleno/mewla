//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	accR uint32 = 4
	accW uint32 = 2
	accX uint32 = 1
)

// accessPath answers access(2)'s question on Windows, where the ACL of a
// user's own folders decides: it reads the entry, and for W on a directory
// makes and removes a file in it. An executable is a regular .exe file.
func accessPath(path string, mode uint32) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if mode&accW != 0 {
		if !info.IsDir() {
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			return file.Close()
		}
		probe, err := os.CreateTemp(path, ".mewla-access-*")
		if err != nil {
			return err
		}
		name := probe.Name()
		probe.Close()
		_ = os.Remove(name)
	}
	if mode&accX != 0 && !info.IsDir() && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errors.New("not an .exe file")
	}
	return nil
}

// bootOwnerOfInfo: files under the user's profile are the user's; Windows
// has no uid on a file to compare.
func bootOwnerOfInfo(os.FileInfo) bootProcessOwner { return bootProcessOwnerSameUID }

// bootExecutableMode: Windows has no execute bits; accessPath checks .exe.
func bootExecutableMode(os.FileInfo) bool { return true }

// detachedSysProcAttr gives a child no console and its own process group, so
// closing the terminal or the SSH session does not end it.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
}

// notifyTerminalResize: a Windows console has no resize signal; mewla attach
// keeps the size it started with until the ConPTY host follows the console.
func notifyTerminalResize(chan<- os.Signal) {}
