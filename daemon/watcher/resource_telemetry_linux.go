package watcher

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

func (s *ResourceSampler) samplePlatform(now time.Time) MachineResourceSnapshot {
	snap := MachineResourceSnapshot{Disks: []DiskResource{}, Consumers: []ProcessConsumer{}}
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		current := parseCPUStat(string(raw))
		cpu := &CPUResource{}
		if old, ok := s.previousCPU["cpu"]; ok {
			if percent, ok := cpuDelta(old, current["cpu"]); ok {
				cpu.UtilizationPercent = &percent
			}
		}
		cores := make([]string, 0, len(current))
		for name := range current {
			if name != "cpu" {
				cores = append(cores, name)
			}
		}
		sort.Slice(cores, func(i, j int) bool {
			a, _ := strconv.Atoi(strings.TrimPrefix(cores[i], "cpu"))
			b, _ := strconv.Atoi(strings.TrimPrefix(cores[j], "cpu"))
			return a < b
		})
		for _, name := range cores {
			if old, ok := s.previousCPU[name]; ok {
				if p, ok := cpuDelta(old, current[name]); ok {
					cpu.PerCorePercent = append(cpu.PerCorePercent, p)
				}
			}
		}
		if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
			f := strings.Fields(string(raw))
			if len(f) >= 3 {
				cpu.Load1, _ = strconv.ParseFloat(f[0], 64)
				cpu.Load5, _ = strconv.ParseFloat(f[1], 64)
				cpu.Load15, _ = strconv.ParseFloat(f[2], 64)
			}
		}
		snap.CPU = cpu
		s.previousCPU = current
	} else {
		snap.Unavailable = append(snap.Unavailable, "cpu")
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		m := parseMemoryInfo(string(raw))
		snap.Memory = &m
	} else {
		snap.Unavailable = append(snap.Unavailable, "memory")
	}
	psi := &PressureSnapshot{}
	for _, item := range []struct {
		name   string
		target *PressureResource
	}{{"cpu", &psi.CPU}, {"memory", &psi.Memory}, {"io", &psi.IO}} {
		if raw, err := os.ReadFile("/proc/pressure/" + item.name); err == nil {
			*item.target = ParsePSI(string(raw))
		} else {
			snap.Unavailable = append(snap.Unavailable, "psi."+item.name)
		}
	}
	snap.PSI = psi
	elapsed := now.Sub(s.previousAt).Seconds()
	currentIO := map[string]ioCounters{}
	if raw, err := os.ReadFile("/proc/diskstats"); err == nil {
		currentIO = parseDiskStats(string(raw))
	}
	if raw, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(string(raw), "\n") {
			sections := strings.SplitN(line, " - ", 2)
			if len(sections) != 2 {
				continue
			}
			left, right := strings.Fields(sections[0]), strings.Fields(sections[1])
			if len(left) < 5 || len(right) < 2 {
				continue
			}
			mount := unescapeMount(left[4])
			fs := right[0]
			if mount != "/" && fs != "ext4" && fs != "xfs" && fs != "btrfs" && fs != "zfs" {
				continue
			}
			dev := left[2]
			if seen[dev] {
				continue
			}
			seen[dev] = true
			var st syscall.Statfs_t
			if syscall.Statfs(mount, &st) != nil {
				continue
			}
			d := DiskResource{Mount: mount, TotalBytes: st.Blocks * uint64(st.Bsize), FreeBytes: st.Bavail * uint64(st.Bsize), UsedBytes: (st.Blocks - st.Bfree) * uint64(st.Bsize)}
			if old, ok := s.previousIO[dev]; ok && elapsed > 0 {
				if cur, ok := currentIO[dev]; ok && cur.Read >= old.Read && cur.Write >= old.Write {
					r := float64(cur.Read-old.Read) / elapsed
					w := float64(cur.Write-old.Write) / elapsed
					d.ReadBytesPerSecond = &r
					d.WriteBytesPerSecond = &w
				}
			}
			snap.Disks = append(snap.Disks, d)
		}
	}
	s.previousIO = currentIO
	records, err := workerproc.Processes(false)
	if err == nil {
		snap.Consumers = processConsumers(records, s.previousProcesses, elapsed)
		s.previousProcesses = records
	} else {
		snap.Unavailable = append(snap.Unavailable, "processes")
	}
	return snap
}
func ParsePSI(data string) PressureResource {
	var p PressureResource
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		a := &PressureAverages{}
		valid := 0
		for _, field := range f[1:] {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 {
				continue
			}
			n, err := strconv.ParseFloat(kv[1], 64)
			if err != nil || n < 0 {
				continue
			}
			switch kv[0] {
			case "avg10":
				a.Avg10 = n
				valid++
			case "avg60":
				a.Avg60 = n
				valid++
			case "avg300":
				a.Avg300 = n
				valid++
			}
		}
		if valid != 3 {
			continue
		}
		switch f[0] {
		case "some":
			p.Some = a
		case "full":
			p.Full = a
		}
	}
	return p
}
func parseCPUStat(raw string) map[string]cpuCounters {
	out := map[string]cpuCounters{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		var c cpuCounters
		for i, v := range f[1:] {
			if i >= 8 {
				break
			}
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				continue
			}
			c.Total += n
			if i == 3 || i == 4 {
				c.Idle += n
			}
		}
		out[f[0]] = c
	}
	return out
}
func parseMemoryInfo(raw string) MemoryResource {
	vals := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		vals[strings.TrimSuffix(f[0], ":")] = n * 1024
	}
	total, avail := vals["MemTotal"], vals["MemAvailable"]
	if avail > total {
		avail = total
	}
	swapFree := vals["SwapFree"]
	if swapFree > vals["SwapTotal"] {
		swapFree = vals["SwapTotal"]
	}
	return MemoryResource{TotalBytes: total, AvailableBytes: avail, UsedBytes: total - avail, CacheBytes: vals["Cached"] + vals["SReclaimable"], SharedBytes: vals["Shmem"], SwapTotalBytes: vals["SwapTotal"], SwapUsedBytes: vals["SwapTotal"] - swapFree}
}
func parseDiskStats(raw string) map[string]ioCounters {
	out := map[string]ioCounters{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		r, e1 := strconv.ParseUint(f[5], 10, 64)
		w, e2 := strconv.ParseUint(f[9], 10, 64)
		if e1 == nil && e2 == nil {
			out[f[0]+":"+f[1]] = ioCounters{r * 512, w * 512}
		}
	}
	return out
}
func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(s)
}
