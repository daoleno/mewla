//go:build darwin

package watcher

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/daoleno/mewla/daemon/workerproc"
	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// The macOS sampler reads the kernel directly, with no shell command in the
// loop: sysctl through x/sys, and the Mach, libproc and IOKit calls the same
// way screen/ reaches macOS frameworks, through purego without cgo. Each call
// fills a Go-owned buffer that resource_macos.go decodes. Libraries are opened
// once and kept for the life of the process: closing them while the runtime
// runs is what crashes other purego users.

type macLib struct {
	hostSelf, taskSelf uint32
	pageSize           uint64
	numer, denom       uint32

	hostProcessorInfo func(host uint32, flavor int32, cpus *uint32, info unsafe.Pointer, infoCount *uint32) int32
	hostStatistics64  func(host uint32, flavor int32, out unsafe.Pointer, count *uint32) int32
	vmDeallocate      func(task uint32, address, size uintptr) int32
	procPidInfo       func(pid, flavor int32, arg uint64, buffer unsafe.Pointer, size int32) int32
	procPidPath       func(pid int32, buffer unsafe.Pointer, size uint32) int32

	// IOKit and CoreFoundation serve disk counters alone; without them only
	// the disk rate is unavailable.
	disk *macDiskLib
}

type macDiskLib struct {
	ioServiceMatching            func(name string) uintptr
	ioServiceGetMatchingServices func(mainPort uint32, matching uintptr, existing *uint32) int32
	ioIteratorNext               func(iterator uint32) uint32
	ioRegistryEntryCreateCFProp  func(entry uint32, key, allocator uintptr, options uint32) uintptr
	ioObjectRelease              func(object uint32) int32
	cfStringCreateWithCString    func(allocator uintptr, cStr string, encoding uint32) uintptr
	cfDictionaryGetValue         func(dict, key uintptr) uintptr
	cfNumberGetValue             func(number uintptr, kind int64, out unsafe.Pointer) bool
	cfRelease                    func(object uintptr)

	statistics, bytesRead, bytesWritten uintptr
}

var macNative struct {
	once sync.Once
	lib  *macLib
	err  error
}

const (
	// mach/processor_info.h, mach/host_info.h, sys/proc_info.h.
	macProcessorCPULoadInfo = 2
	macHostVMInfo64         = 4
	macProcTaskAllInfo      = 2
	macProcPathMax          = 4096
	macTaskAllInfoSize      = macBSDInfoSize + macTaskInfoSize
	cfStringEncodingUTF8    = 0x08000100
	cfNumberSInt64Type      = 4
)

func loadMacNative() (*macLib, error) {
	macNative.once.Do(func() {
		macNative.lib, macNative.err = openMacLib()
		if macNative.err != nil {
			log.Printf("resource telemetry: native macOS sampling unavailable: %v", macNative.err)
		}
	})
	return macNative.lib, macNative.err
}

func openMacLib() (*macLib, error) {
	system, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	lib := &macLib{}
	var missing []string
	bind := func(handle uintptr, fn any, name string) bool {
		if _, err := purego.Dlsym(handle, name); err != nil {
			missing = append(missing, name)
			return false
		}
		purego.RegisterLibFunc(fn, handle, name)
		return true
	}
	var hostSelf, taskSelf func() uint32
	var hostPageSize func(host uint32, size unsafe.Pointer) int32
	var timebase func(info unsafe.Pointer) int32
	bind(system, &hostSelf, "mach_host_self")
	bind(system, &taskSelf, "mach_task_self")
	bind(system, &hostPageSize, "host_page_size")
	bind(system, &timebase, "mach_timebase_info")
	bind(system, &lib.hostProcessorInfo, "host_processor_info")
	bind(system, &lib.hostStatistics64, "host_statistics64")
	bind(system, &lib.vmDeallocate, "vm_deallocate")
	bind(system, &lib.procPidInfo, "proc_pidinfo")
	bind(system, &lib.procPidPath, "proc_pidpath")
	if len(missing) > 0 {
		return nil, fmt.Errorf("libSystem lacks %v", missing)
	}
	// mach_host_self adds a port reference per call: ask once and keep it.
	lib.hostSelf, lib.taskSelf = hostSelf(), taskSelf()
	size := new(uint64)
	if hostPageSize(lib.hostSelf, unsafe.Pointer(size)) != 0 || *size == 0 {
		return nil, fmt.Errorf("host_page_size failed")
	}
	lib.pageSize = *size
	base := new([2]uint32)
	if timebase(unsafe.Pointer(base)) != 0 || base[1] == 0 {
		return nil, fmt.Errorf("mach_timebase_info failed")
	}
	lib.numer, lib.denom = base[0], base[1]

	lib.disk, err = openMacDiskLib()
	if err != nil {
		log.Printf("resource telemetry: disk throughput unavailable: %v", err)
	}
	return lib, nil
}

func openMacDiskLib() (*macDiskLib, error) {
	iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	d := &macDiskLib{}
	var missing []string
	bind := func(handle uintptr, fn any, name string) {
		if _, err := purego.Dlsym(handle, name); err != nil {
			missing = append(missing, name)
			return
		}
		purego.RegisterLibFunc(fn, handle, name)
	}
	bind(iokit, &d.ioServiceMatching, "IOServiceMatching")
	bind(iokit, &d.ioServiceGetMatchingServices, "IOServiceGetMatchingServices")
	bind(iokit, &d.ioIteratorNext, "IOIteratorNext")
	bind(iokit, &d.ioRegistryEntryCreateCFProp, "IORegistryEntryCreateCFProperty")
	bind(iokit, &d.ioObjectRelease, "IOObjectRelease")
	bind(foundation, &d.cfStringCreateWithCString, "CFStringCreateWithCString")
	bind(foundation, &d.cfDictionaryGetValue, "CFDictionaryGetValue")
	bind(foundation, &d.cfNumberGetValue, "CFNumberGetValue")
	bind(foundation, &d.cfRelease, "CFRelease")
	if len(missing) > 0 {
		return nil, fmt.Errorf("IOKit or CoreFoundation lacks %v", missing)
	}
	// The three keys live as long as the process; creating them per sample
	// would only add releases to get wrong.
	d.statistics = d.cfStringCreateWithCString(0, "Statistics", cfStringEncodingUTF8)
	d.bytesRead = d.cfStringCreateWithCString(0, "Bytes (Read)", cfStringEncodingUTF8)
	d.bytesWritten = d.cfStringCreateWithCString(0, "Bytes (Write)", cfStringEncodingUTF8)
	if d.statistics == 0 || d.bytesRead == 0 || d.bytesWritten == 0 {
		return nil, fmt.Errorf("CFStringCreateWithCString failed")
	}
	return d, nil
}

// cpuLoad reads the per-CPU tick counters. host_processor_info allocates the
// array in this task, so it is copied and handed back with vm_deallocate.
func (l *macLib) cpuLoad() []cpuCounters {
	var cpus, count uint32
	var info unsafe.Pointer
	if l.hostProcessorInfo(l.hostSelf, macProcessorCPULoadInfo, &cpus, unsafe.Pointer(&info), &count) != 0 || info == nil {
		return nil
	}
	defer l.vmDeallocate(l.taskSelf, uintptr(info), uintptr(count)*4)
	if count == 0 || count != cpus*macCPULoadStates {
		return nil
	}
	raw := make([]byte, int(count)*4)
	copy(raw, unsafe.Slice((*byte)(info), len(raw)))
	return macDecodeCPULoad(raw)
}

func (l *macLib) vmStats() (macVMStats, bool) {
	// 256 bytes cover vm_statistics64 (160) with room for newer revisions; the
	// kernel writes at most the count it is given.
	words := make([]uint64, 32)
	count := uint32(len(words) * 2)
	if l.hostStatistics64(l.hostSelf, macHostVMInfo64, unsafe.Pointer(&words[0]), &count) != 0 || int(count)*4 > len(words)*8 {
		return macVMStats{}, false
	}
	return macDecodeVM(unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), int(count)*4))
}

// procInfo describes one process; the path is read only for the daemon's own
// user, whose processes are the ones listed.
func (l *macLib) procInfo(pid int, uid int, buffer []byte, path []byte) (macProcInfo, bool) {
	if l.procPidInfo(int32(pid), macProcTaskAllInfo, 0, unsafe.Pointer(&buffer[0]), macTaskAllInfoSize) != macTaskAllInfoSize {
		return macProcInfo{}, false
	}
	_, owner, ok := macDecodeBSDUID(buffer[:macBSDInfoSize])
	task, taskOK := macDecodeTaskInfo(buffer[macBSDInfoSize:macTaskAllInfoSize])
	if !ok || !taskOK {
		return macProcInfo{}, false
	}
	info := macProcInfo{UID: int(owner), Ticks: machToTicks(task.UserTime+task.SystemTime, l.numer, l.denom)}
	if info.UID == uid {
		if n := l.procPidPath(int32(pid), unsafe.Pointer(&path[0]), uint32(len(path))); n > 0 && int(n) <= len(path) {
			info.Path = string(path[:n])
		}
	}
	return info, true
}

// diskCounters sums the byte counters of every IOBlockStorageDriver, the
// driver of each physical or disk-image block device.
func (d *macDiskLib) diskCounters() (ioCounters, bool) {
	matching := d.ioServiceMatching("IOBlockStorageDriver")
	if matching == 0 {
		return ioCounters{}, false
	}
	var iterator uint32
	// IOServiceGetMatchingServices consumes the matching dictionary.
	if d.ioServiceGetMatchingServices(0, matching, &iterator) != 0 {
		return ioCounters{}, false
	}
	defer d.ioObjectRelease(iterator)
	var total ioCounters
	found := false
	number := new(int64)
	for {
		entry := d.ioIteratorNext(iterator)
		if entry == 0 {
			break
		}
		stats := d.ioRegistryEntryCreateCFProp(entry, d.statistics, 0, 0)
		if stats != 0 {
			read, readOK := d.number(stats, d.bytesRead, number)
			written, writtenOK := d.number(stats, d.bytesWritten, number)
			if readOK && writtenOK {
				total.Read += read
				total.Write += written
				found = true
			}
			d.cfRelease(stats)
		}
		d.ioObjectRelease(entry)
	}
	return total, found
}

func (d *macDiskLib) number(dict, key uintptr, out *int64) (uint64, bool) {
	value := d.cfDictionaryGetValue(dict, key)
	if value == 0 {
		return 0, false
	}
	*out = 0
	if !d.cfNumberGetValue(value, cfNumberSInt64Type, unsafe.Pointer(out)) || *out < 0 {
		return 0, false
	}
	return uint64(*out), true
}

func macMounts() []macMount {
	count, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || count <= 0 {
		return nil
	}
	list := make([]unix.Statfs_t, count)
	count, err = unix.Getfsstat(list, unix.MNT_NOWAIT)
	if err != nil || count <= 0 {
		return nil
	}
	mounts := make([]macMount, 0, count)
	for _, st := range list[:min(count, len(list))] {
		mounts = append(mounts, macMount{
			Path: cString(st.Mntonname[:]), FSType: cString(st.Fstypename[:]), Device: cString(st.Mntfromname[:]),
			Local: st.Flags&unix.MNT_LOCAL != 0, Hidden: st.Flags&unix.MNT_DONTBROWSE != 0,
			Blocks: st.Blocks, Free: st.Bfree, Avail: st.Bavail, BlockSize: uint64(st.Bsize),
		})
	}
	return mounts
}

func cString(raw []byte) string {
	if end := bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}
	return string(raw)
}

func (s *ResourceSampler) samplePlatform(now time.Time) (snap MachineResourceSnapshot) {
	var r macReadings
	defer func() {
		// A Go-side fault in a decoder must not take the daemon down with it.
		if recovered := recover(); recovered != nil {
			log.Printf("resource telemetry: macOS sample failed: %v", recovered)
			snap = MachineResourceSnapshot{Disks: []DiskResource{}, Consumers: []ProcessConsumer{}, Unavailable: []string{"cpu", "memory", "psi", "disks", "processes"}}
		}
	}()
	r.Mounts = macMounts()
	if raw, err := unix.SysctlRaw("vm.loadavg"); err == nil {
		if load, ok := macDecodeLoad(raw); ok {
			r.Load = &load
		}
	}
	if raw, err := unix.SysctlRaw("vm.swapusage"); err == nil {
		r.SwapTotal, r.SwapUsed, r.SwapOK = macDecodeSwap(raw)
	}
	if total, err := unix.SysctlUint64("hw.memsize"); err == nil {
		r.TotalMem = total
	}
	if level, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		r.Pressure, _ = macPressureLevel(level)
	}
	if free, err := unix.SysctlUint32("kern.memorystatus_level"); err == nil && free <= 100 {
		percent := float64(free)
		r.FreePercent = &percent
	}
	if lib, err := loadMacNative(); err == nil {
		r.CPU = lib.cpuLoad()
		if vm, ok := lib.vmStats(); ok {
			r.VM, r.PageSize = &vm, lib.pageSize
		}
		if lib.disk != nil {
			if counters, ok := lib.disk.diskCounters(); ok {
				r.DiskIO = &counters
			}
		}
		if records, err := workerproc.Processes(false); err == nil {
			info, path := make([]byte, macTaskAllInfoSize), make([]byte, macProcPathMax)
			uid := os.Getuid()
			r.Processes = macEnrichProcesses(records, func(pid int) (macProcInfo, bool) { return lib.procInfo(pid, uid, info, path) })
		}
	}
	return s.buildMacSnapshot(now, r)
}
