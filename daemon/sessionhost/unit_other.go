//go:build !linux

package sessionhost

import (
	"os"
	"os/exec"
)

// hostUnitCommand: only Linux has the systemd user units the host runs in.
func hostUnitCommand(StartOptions, []string, *os.File) (*exec.Cmd, bool) { return nil, false }
