package workerproc

import (
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// Environ returns pid's environment from kern.procargs2, or nil when it
// cannot be read.
func Environ(pid int) []string {
	if pid <= 0 {
		return nil
	}
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil
	}
	return parseProcArgsEnvironment(raw)
}

// Descendants returns every process below root, breadth first, from one ps
// snapshot of the process tree.
//
// ps lists itself, as a child of whoever ran it. By the time the caller uses
// the list that pid is gone, and a lsof aimed at it exits 1, so ps's own pid
// is left out.
func Descendants(root int) []int {
	command := procspawn.Command("ps ppid", procspawn.SystemTool("ps"), "-axo", "pid=,ppid=")
	out, err := command.Output()
	if err != nil {
		return nil
	}
	self := 0
	if command.Process != nil {
		self = command.Process.Pid
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil || pid <= 0 || pid == self {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	for _, pids := range children {
		sort.Ints(pids)
	}
	return descendants(root, func(pid int) []int { return children[pid] })
}

// OpenFiles is unavailable without /proc; callers fall back to lsof.
func OpenFiles(int) []string {
	return nil
}
