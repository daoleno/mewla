//go:build linux || darwin

package workerproc

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process identity includes the kernel start time to fence PID reuse. Args is
// an executable label, never an environment or an unredacted command line.
type Process struct {
	PID, PPID, PGID, SID                    int
	Start                                   string
	RSS, Ticks                              uint64
	UID                                     int
	Args, ResourceID, WorkerID, ContainerID string
}
type ProcessIdentity struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

// Existing portable ps parsers are also used by the Darwin reader.
type processRecord = Process

var ownershipRegistry struct {
	sync.RWMutex
	entries map[ProcessIdentity]string
}

var processCache struct {
	sync.Mutex
	at        time.Time
	processes map[int]Process
}

func Processes(fresh bool) (map[int]Process, error) {
	processCache.Lock()
	defer processCache.Unlock()
	if !fresh && processCache.processes != nil && time.Since(processCache.at) < 2*time.Second {
		return processCache.processes, nil
	}
	records, err := readProcesses(processCache.processes)
	if err != nil {
		return nil, err
	}
	ownershipRegistry.Lock()
	for id := range ownershipRegistry.entries {
		p, ok := records[id.PID]
		if !ok || p.Start != id.Start {
			delete(ownershipRegistry.entries, id)
		}
	}
	for pid, p := range records {
		if p.ResourceID == "" {
			if token := ownershipRegistry.entries[ProcessIdentity{pid, p.Start}]; token != "" {
				p.ResourceID = token
				records[pid] = p
			}
		}
	}
	ownershipRegistry.Unlock()
	for changed := true; changed; {
		changed = false
		for pid, p := range records {
			if p.ResourceID == "" {
				if parent, ok := records[p.PPID]; ok && parent.ResourceID != "" {
					p.ResourceID = parent.ResourceID
					records[pid] = p
					changed = true
				}
			}
		}
	}
	processCache.at = time.Now()
	processCache.processes = records
	return records, nil
}

// Owned follows marked processes, recorded exact identities, and their children.
// It never adopts an unverified numeric process group or command-line substring.
func Owned(records map[int]Process, lease Lease) map[int]Process {
	result := map[int]Process{}
	if lease.BootID != "" && lease.BootID != bootID() {
		return result
	}
	for pid, p := range records {
		if lease.ResourceID != "" && p.ResourceID == lease.ResourceID {
			result[pid] = p
		}
	}
	for _, id := range lease.Observed {
		if p, ok := records[id.PID]; ok && id.Start != "" && p.Start == id.Start {
			result[p.PID] = p
		}
	}
	for changed := true; changed; {
		changed = false
		for pid, p := range records {
			if _, ok := result[pid]; ok {
				continue
			}
			if _, ok := result[p.PPID]; ok {
				result[pid] = p
				changed = true
			}
		}
	}
	return result
}
func ObserveLeases(dir string) error {
	leases, err := ListLeases(dir)
	if err != nil {
		return err
	}
	ownershipRegistry.Lock()
	if ownershipRegistry.entries == nil {
		ownershipRegistry.entries = map[ProcessIdentity]string{}
	}
	for _, lease := range leases {
		if lease.BootID != "" && lease.BootID != bootID() {
			continue
		}
		for _, id := range lease.Observed {
			ownershipRegistry.entries[id] = lease.ResourceID
		}
	}
	ownershipRegistry.Unlock()
	records, err := Processes(false)
	if err != nil {
		return err
	}
	for _, lease := range leases {
		owned := Owned(records, lease)
		ids := make([]ProcessIdentity, 0, len(owned))
		for _, p := range owned {
			if p.Start != "" {
				ids = append(ids, ProcessIdentity{p.PID, p.Start})
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].PID < ids[j].PID })
		same := len(ids) == len(lease.Observed)
		if same {
			for i := range ids {
				if ids[i] != lease.Observed[i] {
					same = false
					break
				}
			}
		}
		if same {
			continue
		}
		lease.Observed = ids
		path, err := LeasePath(dir, lease.ResourceID)
		if err != nil {
			return err
		}
		if err = writeLease(path, lease); err != nil {
			return err
		}
	}
	return nil
}
func StopLease(path string) error {
	lease, err := ReadLease(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := StopOwned(lease, nil); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// StopOwned is called only for explicit lifecycle cleanup. Protected processes
// (the live provider and its ancestor chain for resource release) survive.
func StopOwned(lease Lease, protected map[int]bool) error {
	records, err := Processes(true)
	if err != nil {
		return err
	}
	owned := Owned(records, lease)
	// Never terminate the calling control plane or its ancestors.
	for pid := os.Getpid(); pid > 1; {
		if protected == nil {
			protected = map[int]bool{}
		}
		protected[pid] = true
		p, ok := records[pid]
		if !ok || p.PPID == pid {
			break
		}
		pid = p.PPID
	}
	for pid, p := range owned {
		if protected[pid] {
			delete(owned, pid)
			continue
		}
		lease.Observed = append(lease.Observed, ProcessIdentity{pid, p.Start})
	}
	signalExact(owned, syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		records, err = Processes(true)
		if err != nil {
			return err
		}
		owned = Owned(records, lease)
		for pid := range protected {
			delete(owned, pid)
		}
		if len(owned) == 0 {
			return nil
		}
	}
	signalExact(owned, syscall.SIGKILL)
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		records, err = Processes(true)
		if err != nil {
			return err
		}
		owned = Owned(records, lease)
		for pid := range protected {
			delete(owned, pid)
		}
		if len(owned) == 0 {
			return nil
		}
	}
	return fmt.Errorf("%d owned processes remain after cleanup", len(owned))
}
func signalExact(records map[int]Process, signal syscall.Signal) {
	for pid, p := range records {
		if pid <= 1 || p.Start == "" {
			continue
		}
		if processIdentityMatches(p) {
			_ = syscall.Kill(pid, signal)
		}
	}
}

// workerEnvIdentity reads the delegated Worker identity from a process
// environment.
func workerEnvIdentity(entries []string) (resourceID, workerID string) {
	values := map[string]string{}
	for _, entry := range entries {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	return values["MEWLA_WORKER_RESOURCE_UNIT"], values["MEWLA_WORKER_ID"]
}
