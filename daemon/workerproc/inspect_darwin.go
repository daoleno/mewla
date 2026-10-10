package workerproc

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
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
func Descendants(root int) []int {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil || pid <= 0 {
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
