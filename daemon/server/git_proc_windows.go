//go:build windows

package server

import "os/exec"

func gitOwnProcessGroup(cmd *exec.Cmd) {}
