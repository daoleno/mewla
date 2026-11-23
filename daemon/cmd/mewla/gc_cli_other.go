//go:build !linux && !darwin

package main

import "errors"

// gcSessionProcesses: a Windows Session's processes are its Job Object's; not
// counted here.
func gcSessionProcesses(string, string) int { return -1 }

// gcProveSession: keep and release are not offered on Windows yet.
func gcProveSession(string, string, string) error {
	return errors.New("mewla gc keep and release are not available on Windows yet")
}
