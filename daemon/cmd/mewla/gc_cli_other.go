//go:build !linux && !darwin

package main

// gcSessionProcesses: a Windows Session's processes are its Job Object's; not
// counted here.
func gcSessionProcesses(string, string) int { return -1 }
