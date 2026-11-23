//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strings"

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
