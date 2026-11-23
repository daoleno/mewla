//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/userunit"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// gcSessionProcesses counts the processes the Session's lease owns.
func gcSessionProcesses(root, unit string) int {
	records, err := workerproc.Processes(false)
	if err != nil {
		return -1
	}
	lease := workerproc.Lease{ResourceID: unit}
	owner := strings.TrimSpace(os.Getenv("MEWLA_WORKER_RESOURCE_OWNER"))
	if root != "" && owner != "" {
		if path, err := workerproc.LeasePath(filepath.Join(root, "run", "worker-resources", owner), unit); err == nil {
			if read, err := workerproc.ReadLease(path); err == nil {
				lease = read
			}
		}
	}
	return len(workerproc.Owned(records, lease))
}

// gcProveSession requires the caller to be the Session's by the runtime's
// own record: it runs inside the Session's slice, or one of its ancestors is
// a process the Session's lease recorded (same pid and start time).
func gcProveSession(root, owner, unit string) error {
	if raw, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		if token := userunit.Token(unit); token != "" && strings.Contains(string(raw), "-"+token+".slice/") {
			return nil
		}
	}
	path, err := workerproc.LeasePath(filepath.Join(root, "run", "worker-resources", owner), unit)
	if err != nil {
		return err
	}
	lease, err := workerproc.ReadLease(path)
	if err != nil {
		return fmt.Errorf("%s has no lease: %w", unit, err)
	}
	recorded := map[workerproc.ProcessIdentity]bool{}
	for _, id := range lease.Observed {
		recorded[id] = true
	}
	records, err := workerproc.Processes(true)
	if err != nil {
		return err
	}
	for pid, seen := os.Getpid(), 0; pid > 1 && seen < 64; seen++ {
		p, ok := records[pid]
		if !ok {
			break
		}
		if recorded[workerproc.ProcessIdentity{PID: pid, Start: p.Start}] {
			return nil
		}
		pid = p.PPID
	}
	return fmt.Errorf("this process is not one of %s's: neither in its slice nor started by a process its lease recorded", unit)
}
