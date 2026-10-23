package watcher

import (
	"encoding/binary"
	"math"
	"math/bits"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// The macOS half of the machine snapshot. Nothing here calls the OS: it decodes
// the bytes and numbers resource_telemetry_darwin.go reads, so the layouts and
// the arithmetic are tested on every platform. A wrong offset on a Mac would
// show as a wrong number, or as memory the native call overwrote, so each
// layout names the Apple header it comes from.

// macCPULoadStates is processor_cpu_load_info (mach/processor_info.h): four
// natural_t tick counters per CPU, indexed by CPU_STATE_USER, _SYSTEM, _IDLE
// and _NICE (mach/machine.h).
const macCPULoadStates = 4

// macDecodeCPULoad turns the array host_processor_info(PROCESSOR_CPU_LOAD_INFO)
// returns into one counter per logical CPU.
func macDecodeCPULoad(raw []byte) []cpuCounters {
	cpus := make([]cpuCounters, 0, len(raw)/(4*macCPULoadStates))
	for off := 0; off+4*macCPULoadStates <= len(raw); off += 4 * macCPULoadStates {
		var ticks [macCPULoadStates]uint64
		for i := range ticks {
			ticks[i] = uint64(binary.LittleEndian.Uint32(raw[off+4*i:]))
		}
		cpus = append(cpus, cpuCounters{Total: ticks[0] + ticks[1] + ticks[2] + ticks[3], Idle: ticks[2]})
	}
	return cpus
}

func macSumCPU(cpus []cpuCounters) cpuCounters {
	var sum cpuCounters
	for _, c := range cpus {
		sum.Total += c.Total
		sum.Idle += c.Idle
	}
	return sum
}

// macVMStats holds the vm_statistics64 page counts the memory figures use.
type macVMStats struct {
	Free, Active, Inactive, Wired, Purgeable, Speculative uint64
	Compressor, External, Internal                        uint64
}

// vm_statistics64 (osfmk/mach/vm_statistics.h), 4-byte natural_t counters and
// 8-byte counters interleaved: the offsets below are that struct's.
const (
	macVMFreeOffset        = 0
	macVMActiveOffset      = 4
	macVMInactiveOffset    = 8
	macVMWireOffset        = 12
	macVMPurgeableOffset   = 88
	macVMSpeculativeOffset = 92
	macVMCompressorOffset  = 128
	macVMExternalOffset    = 136
	macVMInternalOffset    = 140
	// macVMMinimumSize ends after internal_page_count, the last field read.
	macVMMinimumSize = 144
)

func macDecodeVM(raw []byte) (macVMStats, bool) {
	if len(raw) < macVMMinimumSize {
		return macVMStats{}, false
	}
	at := func(off int) uint64 { return uint64(binary.LittleEndian.Uint32(raw[off:])) }
	return macVMStats{
		Free: at(macVMFreeOffset), Active: at(macVMActiveOffset), Inactive: at(macVMInactiveOffset), Wired: at(macVMWireOffset),
		Purgeable: at(macVMPurgeableOffset), Speculative: at(macVMSpeculativeOffset),
		Compressor: at(macVMCompressorOffset), External: at(macVMExternalOffset), Internal: at(macVMInternalOffset),
	}, true
}

// macMemory follows Activity Monitor: used is app memory (anonymous pages that
// are not purgeable) plus wired plus compressed; cached files are file-backed
// plus purgeable pages. Available is what the machine could hand out without
// pressure, total minus used. Shared memory is not reported on macOS.
func macMemory(vm macVMStats, pageSize, total uint64) MemoryResource {
	app := uint64(0)
	if vm.Internal > vm.Purgeable {
		app = vm.Internal - vm.Purgeable
	}
	used := (app + vm.Wired + vm.Compressor) * pageSize
	if used > total {
		used = total
	}
	return MemoryResource{
		TotalBytes:     total,
		AvailableBytes: total - used,
		UsedBytes:      used,
		CacheBytes:     (vm.External + vm.Purgeable) * pageSize,
	}
}

// macDecodeSwap reads xsw_usage (sys/sysctl.h): xsu_total, xsu_avail, xsu_used,
// then a 4-byte page size and a flag, 32 bytes in all.
func macDecodeSwap(raw []byte) (total, used uint64, ok bool) {
	if len(raw) != 32 {
		return 0, 0, false
	}
	total, used = binary.LittleEndian.Uint64(raw[0:]), binary.LittleEndian.Uint64(raw[16:])
	if used > total {
		used = total
	}
	return total, used, true
}

// macDecodeLoad reads struct loadavg (sys/vm.h): three 32-bit fixed-point
// averages, padding, and the 64-bit scale they are divided by.
func macDecodeLoad(raw []byte) (load [3]float64, ok bool) {
	if len(raw) < 24 {
		return load, false
	}
	scale := binary.LittleEndian.Uint64(raw[16:])
	if scale == 0 {
		return load, false
	}
	for i := range load {
		load[i] = float64(binary.LittleEndian.Uint32(raw[4*i:])) / float64(scale)
	}
	return load, true
}

// macPressureLevel maps kern.memorystatus_vm_pressure_level, which carries the
// DISPATCH_MEMORYPRESSURE_* flag values (libdispatch dispatch/source.h): 1
// normal, 2 warn, 4 critical. Any other value is unknown, not normal.
func macPressureLevel(level uint32) (string, bool) {
	switch level {
	case 1:
		return "normal", true
	case 2:
		return "warn", true
	case 4:
		return "critical", true
	}
	return "", false
}

// proc_taskinfo and proc_bsdinfo (bsd/sys/proc_info.h) have no packing.
const (
	macTaskInfoSize = 96
	macBSDInfoSize  = 136
)

type macTaskInfo struct {
	Resident, UserTime, SystemTime uint64 // times in Mach absolute time units
}

func macDecodeTaskInfo(raw []byte) (macTaskInfo, bool) {
	if len(raw) < macTaskInfoSize {
		return macTaskInfo{}, false
	}
	return macTaskInfo{
		Resident:   binary.LittleEndian.Uint64(raw[8:]),
		UserTime:   binary.LittleEndian.Uint64(raw[16:]),
		SystemTime: binary.LittleEndian.Uint64(raw[24:]),
	}, true
}

// macDecodeBSDUID returns pbi_pid and pbi_uid from proc_bsdinfo.
func macDecodeBSDUID(raw []byte) (pid, uid uint32, ok bool) {
	if len(raw) < macBSDInfoSize {
		return 0, 0, false
	}
	return binary.LittleEndian.Uint32(raw[12:]), binary.LittleEndian.Uint32(raw[20:]), true
}

// machToTicks converts Mach absolute time to the 100 Hz ticks processConsumers
// counts. numer/denom is mach_timebase_info: 1/1 on Intel, 125/3 on Apple
// silicon, where the product leaves 64 bits.
func machToTicks(mach uint64, numer, denom uint32) uint64 {
	if denom == 0 {
		return 0
	}
	hi, lo := bits.Mul64(mach, uint64(numer))
	if hi >= uint64(denom) {
		return math.MaxUint64
	}
	nanoseconds, _ := bits.Div64(hi, lo, uint64(denom))
	return nanoseconds / uint64(time.Second/100)
}

// macProcInfo is what libproc says about one process.
type macProcInfo struct {
	UID  int
	Path string
	// Ticks is user plus system CPU time in 100 Hz ticks.
	Ticks uint64
}

// macProcessLabel is the executable's name, as Linux reports it: telemetry
// never carries arguments. The command line is only a fallback for the first
// word when the path cannot be read.
func macProcessLabel(path, command string) string {
	if path != "" {
		return filepath.Base(path)
	}
	if fields := strings.Fields(command); len(fields) > 0 {
		return filepath.Base(fields[0])
	}
	return "unknown"
}

// macEnrichProcesses completes workerproc's macOS records, which carry neither
// UID nor CPU time and whose Args is the whole command line. It returns copies:
// workerproc keeps its own records, and its Start tokens, untouched. A process
// libproc will not describe (another user's) gets UID -1 so it is not listed.
func macEnrichProcesses(records map[int]workerproc.Process, lookup func(pid int) (macProcInfo, bool)) map[int]workerproc.Process {
	out := make(map[int]workerproc.Process, len(records))
	for pid, p := range records {
		info, ok := lookup(pid)
		if !ok {
			p.UID, p.Args = -1, macProcessLabel("", p.Args)
			out[pid] = p
			continue
		}
		p.UID, p.Ticks, p.Args = info.UID, info.Ticks, macProcessLabel(info.Path, p.Args)
		out[pid] = p
	}
	return out
}

// macMount is one mounted filesystem as statfs reports it.
type macMount struct {
	Path, FSType, Device string
	Local, Hidden        bool
	Blocks, Free, Avail  uint64
	BlockSize            uint64
}

var macContainerRE = regexp.MustCompile(`^(?:/dev/)?(disk\d+)`)

// macDisks lists the startup volume and any other local volume the user
// mounted under /Volumes. Volumes of one APFS container report the container's
// capacity, so a container appears once, as the Linux sampler lists a device
// once. System and hidden volumes are leftovers of the same container.
func macDisks(mounts []macMount) []DiskResource {
	disk := func(m macMount) DiskResource {
		size := m.BlockSize
		return DiskResource{Mount: m.Path, TotalBytes: m.Blocks * size, FreeBytes: m.Avail * size, UsedBytes: (m.Blocks - min(m.Free, m.Blocks)) * size}
	}
	container := func(m macMount) string {
		if match := macContainerRE.FindStringSubmatch(m.Device); match != nil {
			return match[1]
		}
		return m.Device
	}
	out := []DiskResource{}
	seen := map[string]bool{}
	for _, m := range mounts {
		if m.Path == "/" {
			out = append(out, disk(m))
			seen[container(m)] = true
			break
		}
	}
	extra := []macMount{}
	for _, m := range mounts {
		if m.Path == "/" || !m.Local || m.Hidden || !strings.HasPrefix(m.Path, "/Volumes/") {
			continue
		}
		switch m.FSType {
		case "apfs", "hfs", "exfat", "msdos", "ufs":
			extra = append(extra, m)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Path < extra[j].Path })
	for _, m := range extra {
		if seen[container(m)] || m.Blocks == 0 {
			continue
		}
		seen[container(m)] = true
		out = append(out, disk(m))
	}
	return out
}

// macReadings is one pass of raw macOS measurements. A nil or false member is a
// source that failed; it is reported unavailable, never as zero.
type macReadings struct {
	CPU       []cpuCounters
	Load      *[3]float64
	VM        *macVMStats
	PageSize  uint64
	TotalMem  uint64
	SwapTotal uint64
	SwapUsed  uint64
	SwapOK    bool
	Pressure  string
	// FreePercent is kern.memorystatus_level, the kernel's own free-memory
	// percentage, when it could be read.
	FreePercent *float64
	Mounts      []macMount
	// DiskIO sums every physical drive's byte counters.
	DiskIO    *ioCounters
	Processes map[int]workerproc.Process
}

// buildMacSnapshot turns a reading into the snapshot and advances the sampler's
// previous-interval state, as the Linux sampler does for its own sources.
func (s *ResourceSampler) buildMacSnapshot(now time.Time, r macReadings) MachineResourceSnapshot {
	snap := MachineResourceSnapshot{Disks: []DiskResource{}, Consumers: []ProcessConsumer{}}
	// macOS has no pressure-stall information; the kernel's memory pressure
	// level stands in for it, in its own field.
	snap.Unavailable = append(snap.Unavailable, "psi")
	elapsed := now.Sub(s.previousAt).Seconds()

	if len(r.CPU) > 0 {
		current := map[string]cpuCounters{"cpu": macSumCPU(r.CPU)}
		cpu := &CPUResource{}
		if old, ok := s.previousCPU["cpu"]; ok {
			if percent, ok := cpuDelta(old, current["cpu"]); ok {
				cpu.UtilizationPercent = &percent
			}
		}
		for i, c := range r.CPU {
			name := "cpu" + strconv.Itoa(i)
			current[name] = c
			if old, ok := s.previousCPU[name]; ok {
				if p, ok := cpuDelta(old, c); ok {
					cpu.PerCorePercent = append(cpu.PerCorePercent, p)
				}
			}
		}
		if r.Load != nil {
			cpu.Load1, cpu.Load5, cpu.Load15 = r.Load[0], r.Load[1], r.Load[2]
		}
		snap.CPU = cpu
		s.previousCPU = current
	} else {
		snap.Unavailable = append(snap.Unavailable, "cpu")
	}
	if r.Load == nil && len(r.CPU) > 0 {
		snap.Unavailable = append(snap.Unavailable, "cpu.load")
	}

	if r.VM != nil && r.PageSize > 0 && r.TotalMem > 0 {
		m := macMemory(*r.VM, r.PageSize, r.TotalMem)
		if r.SwapOK {
			m.SwapTotalBytes, m.SwapUsedBytes = r.SwapTotal, r.SwapUsed
		} else {
			snap.Unavailable = append(snap.Unavailable, "memory.swap")
		}
		snap.Memory = &m
		snap.Unavailable = append(snap.Unavailable, "memory.shared")
	} else {
		snap.Unavailable = append(snap.Unavailable, "memory")
	}
	if r.Pressure != "" {
		snap.MemoryPressure = &MemoryPressure{Level: r.Pressure, FreePercent: r.FreePercent}
	} else {
		snap.Unavailable = append(snap.Unavailable, "memory_pressure")
	}

	snap.Disks = macDisks(r.Mounts)
	if len(snap.Disks) == 0 {
		snap.Unavailable = append(snap.Unavailable, "disks")
	}
	// The counters are the machine's total across physical drives, so the rate
	// belongs to the startup volume's row, the one that always exists.
	currentIO := map[string]ioCounters{}
	if r.DiskIO != nil {
		currentIO["total"] = *r.DiskIO
		if old, ok := s.previousIO["total"]; ok && elapsed > 0 && len(snap.Disks) > 0 && r.DiskIO.Read >= old.Read && r.DiskIO.Write >= old.Write {
			read := float64(r.DiskIO.Read-old.Read) / elapsed
			write := float64(r.DiskIO.Write-old.Write) / elapsed
			snap.Disks[0].ReadBytesPerSecond, snap.Disks[0].WriteBytesPerSecond = &read, &write
		}
	} else {
		snap.Unavailable = append(snap.Unavailable, "disks.io")
	}
	s.previousIO = currentIO

	if r.Processes != nil {
		snap.Consumers = processConsumers(r.Processes, s.previousProcesses, elapsed)
		s.previousProcesses = r.Processes
	} else {
		snap.Unavailable = append(snap.Unavailable, "processes")
	}
	return snap
}
