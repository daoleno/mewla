//go:build windows

package watcher

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/platform"
)

// snapshotProcessTable reads the process table on Windows, which has no
// process groups or controlling terminals. A Session's program tree there is
// what its `mewla sh` launcher (the session host's direct child) started, so
// the launcher stands in for the pane's foreground process group: it and every
// process below it carry its pid as pgid, and it is its own tpgid. Its script
// is left out of its args, so the launch text never reads as a provider.
func snapshotProcessTable() (map[int]processInfo, error) {
	list, err := platform.ProcessList()
	if err != nil {
		return nil, fmt.Errorf("windows process snapshot: %w", err)
	}
	processes := make(map[int]processInfo, len(list))
	for _, entry := range list {
		if entry.PID <= 0 {
			continue
		}
		info := processInfo{pid: entry.PID, ppid: entry.PPID, pgid: entry.PID, comm: strings.TrimSuffix(entry.Image, filepath.Ext(entry.Image))}
		// Start time and command line only for processes this user may query:
		// a Session's processes always are.
		if started, err := platform.ProcessStartTime(entry.PID); err == nil {
			info.startedAt = started
			if args, err := platform.ProcessCommandLine(entry.PID); err == nil {
				info.args = args
			}
		}
		processes[entry.PID] = info
	}
	if len(processes) == 0 {
		return nil, fmt.Errorf("windows process snapshot listed no processes")
	}
	dropReusedParents(processes)
	leaders := map[int]bool{}
	for pid, info := range processes {
		if launcherProcess(info) {
			leaders[pid] = true
			info.args = info.comm + " sh"
			info.tpgid = pid
			processes[pid] = info
		}
	}
	for pid, info := range processes {
		if leader := nearestLauncher(pid, processes, leaders); leader > 0 {
			info.pgid = leader
			if info.tpgid == 0 {
				info.tpgid = leader
			}
			processes[pid] = info
		}
	}
	return processes, nil
}

// snapshotProcessTableNear: one Toolhelp snapshot lists every process, so
// there is no cheaper partial read.
func snapshotProcessTableNear(_ []int) (map[int]processInfo, error) {
	return snapshotProcessTable()
}

// nearestLauncher walks pid's ancestors to the closest launcher. A parent
// that started after its child is a reused pid, not the parent.
func nearestLauncher(pid int, processes map[int]processInfo, leaders map[int]bool) int {
	current := processes[pid]
	for depth := 0; depth < 64; depth++ {
		if leaders[current.pid] {
			return current.pid
		}
		parent, ok := processes[current.ppid]
		if !ok || parent.pid == current.pid || parent.startedAt.IsZero() || current.startedAt.IsZero() || parent.startedAt.After(current.startedAt) {
			return 0
		}
		current = parent
	}
	return 0
}
