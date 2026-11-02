//go:build !linux && !windows

package connections

import "os/exec"

func cloudParentDeath(cmd *exec.Cmd) {}
