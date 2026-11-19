//go:build !linux && !windows

package watcher

import (
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// preciseProcessStart is when pid started (kinfo_proc, to the microsecond on
// macOS).
func preciseProcessStart(pid int) (time.Time, bool) {
	return workerproc.StartTime(pid)
}
