package watcher

import (
	"os"
	"sort"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// Shared by the Linux and macOS samplers: the platform-neutral half of the
// machine snapshot.

type cpuCounters struct{ Total, Idle uint64 }
type ioCounters struct{ Read, Write uint64 }

func cpuDelta(old, current cpuCounters) (float64, bool) {
	if current.Total <= old.Total || current.Idle < old.Idle {
		return 0, false
	}
	total := current.Total - old.Total
	idle := current.Idle - old.Idle
	if idle > total {
		return 0, false
	}
	return 100 * float64(total-idle) / float64(total), true
}

func processConsumers(records, previous map[int]workerproc.Process, elapsed float64) []ProcessConsumer {
	// Propagate ownership through parent trees, including env-scrubbed children.
	owned := map[int]string{}
	workers := map[int]string{}
	for pid, p := range records {
		if p.ResourceID != "" {
			owned[pid] = p.ResourceID
		}
		if p.WorkerID != "" {
			workers[pid] = p.WorkerID
		}
	}
	for changed := true; changed; {
		changed = false
		for pid, p := range records {
			if owned[pid] == "" && owned[p.PPID] != "" {
				owned[pid] = owned[p.PPID]
				changed = true
			}
			if workers[pid] == "" && workers[p.PPID] != "" {
				workers[pid] = workers[p.PPID]
				changed = true
			}
		}
	}
	groups := map[string]*ProcessConsumer{}
	for pid, p := range records {
		// Root/system services remain in machine totals; the consumer view is user
		// workloads plus observable container workloads.
		if p.UID != os.Getuid() && p.ContainerID == "" {
			continue
		}
		key := "user:" + p.Args
		owner := "user"
		wid := workers[pid]
		if p.ContainerID != "" {
			key = "docker:" + p.ContainerID
			owner = "docker"
		}
		if wid != "" {
			key = "session:" + wid
			owner = "worker"
		}
		if owned[pid] != "" {
			key = owned[pid]
			owner = "worker"
		}
		c := groups[key]
		if c == nil {
			c = &ProcessConsumer{ID: key, Owner: owner, WorkerID: wid, Status: "unknown", Kinds: []string{}, Commands: []string{}}
			groups[key] = c
		}
		c.RSSBytes += p.RSS
		c.ProcessCount++
		c.Processes = append(c.Processes, ConsumerProcess{p.PID, p.Start, p.Args, p.RSS})
		if old, ok := previous[pid]; ok && old.Start == p.Start && p.Ticks >= old.Ticks && elapsed > 0 {
			if c.CPUPercent == nil {
				v := 0.0
				c.CPUPercent = &v
			}
			*c.CPUPercent += float64(p.Ticks-old.Ticks) / elapsed
		} // Linux USER_HZ=100, percent of one core.
		label := p.Args
		if len(label) > 64 {
			label = label[:64]
		}
		if !containsString(c.Commands, label) && len(c.Commands) < 6 {
			c.Commands = append(c.Commands, label)
		}
		for _, kind := range classifyProcess(p.Args) {
			if !containsString(c.Kinds, kind) {
				c.Kinds = append(c.Kinds, kind)
			}
		}
	}
	result := make([]ProcessConsumer, 0, len(groups))
	for _, c := range groups {
		sort.Slice(c.Processes, func(i, j int) bool { return c.Processes[i].RSSBytes > c.Processes[j].RSSBytes })
		if len(c.Processes) > 5 {
			c.Processes = c.Processes[:5]
		}
		result = append(result, *c)
	}
	return result
}
func containsString(items []string, item string) bool {
	for _, s := range items {
		if s == item {
			return true
		}
	}
	return false
}
