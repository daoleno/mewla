//go:build windows

package modelprofiles

import (
	"os"

	"github.com/daoleno/mewla/daemon/platform"
)

// signalProcess ends pid. Windows has no SIGTERM to ask first, so both a
// polite and a forced stop end the process.
func signalProcess(pid int, force bool) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
		_ = p.Release()
	}
}

func processExists(pid int) bool { return platform.ProcessAlive(pid) }
