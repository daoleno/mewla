//go:build !linux

package watcher

import (
	"fmt"
	"time"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// psPath is where macOS and the BSDs keep ps. It is not looked up on PATH: a
// daemon started by launchd, or a test with a narrowed PATH, may not have /bin
// on it, and the process scan must not depend on that.
const psPath = "/bin/ps"

// snapshotProcessTable lists processes with ps where there is no /proc.
func snapshotProcessTable() (map[int]processInfo, error) {
	// ucomm is the executable's accounting name on every platform. macOS
	// comm is argv[0] cut to the 16-column field ("/Users/me/.local/"), so
	// provider checks on the process name would never match there.
	command := procspawn.Command("ps scan", psPath, "-eo", "pid=,ppid=,pgid=,tpgid=,lstart=,ucomm=,args=")
	command.Env = append(command.Environ(), "LC_ALL=C")
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("ps process snapshot: %w%s", err, commandStderr(err))
	}
	processes := parseProcessSnapshot(out, time.Local)
	if len(processes) == 0 {
		return nil, fmt.Errorf("ps process snapshot parsed no processes from %d bytes", len(out))
	}
	return processes, nil
}
