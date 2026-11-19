//go:build windows

package watcher

import (
	"os"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// processGroupsAreReal: Windows has no process groups, so a command's group
// is its process tree as the snapshot found it.
const processGroupsAreReal = false

func preciseProcessStart(pid int) (time.Time, bool) {
	start, err := platform.ProcessStartTime(pid)
	return start, err == nil
}

// signalCommandGroup terminates every process of the command's tree that is
// still the process it was. Windows has no SIGTERM to give a grace with.
func signalCommandGroup(group turnCommandGroup, _ bool) error {
	var first error
	for _, member := range group.members {
		if !processStillThere(member) {
			continue
		}
		process, err := os.FindProcess(member.pid)
		if err == nil {
			err = process.Kill()
		}
		if err != nil && first == nil && processStillThere(member) {
			first = err
		}
	}
	return first
}

func commandGroupAlive(group turnCommandGroup) bool {
	for _, member := range group.members {
		if platform.ProcessAlive(member.pid) && processStillThere(member) {
			return true
		}
	}
	return false
}
