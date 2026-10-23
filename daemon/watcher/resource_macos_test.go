package watcher

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// Fixtures are built field by field at the offsets Apple's headers give, so a
// drifting offset in the decoder fails here, on any platform, rather than on a
// Mac.

func putU32(raw []byte, off int, v uint32) { binary.LittleEndian.PutUint32(raw[off:], v) }
func putU64(raw []byte, off int, v uint64) { binary.LittleEndian.PutUint64(raw[off:], v) }

func TestMacCPULoadDecodesUserSystemIdleNicePerCore(t *testing.T) {
	raw := make([]byte, 32)
	// cpu0: user 10, system 20, idle 60, nice 10. cpu1: idle only.
	for i, v := range []uint32{10, 20, 60, 10, 0, 0, 100, 0} {
		putU32(raw, 4*i, v)
	}
	cpus := macDecodeCPULoad(raw)
	if len(cpus) != 2 || cpus[0] != (cpuCounters{Total: 100, Idle: 60}) || cpus[1] != (cpuCounters{Total: 100, Idle: 100}) {
		t.Fatalf("cpus %+v", cpus)
	}
	if sum := macSumCPU(cpus); sum != (cpuCounters{Total: 200, Idle: 160}) {
		t.Fatalf("sum %+v", sum)
	}
	if len(macDecodeCPULoad(raw[:30])) != 1 {
		t.Fatal("a truncated tail was decoded as a CPU")
	}
}

func TestMacMemoryFollowsActivityMonitor(t *testing.T) {
	raw := make([]byte, 160)
	const page = 16384
	putU32(raw, macVMFreeOffset, 100)
	putU32(raw, macVMActiveOffset, 200)
	putU32(raw, macVMInactiveOffset, 300)
	putU32(raw, macVMWireOffset, 50)
	putU32(raw, macVMPurgeableOffset, 20)
	putU32(raw, macVMSpeculativeOffset, 10)
	putU32(raw, macVMCompressorOffset, 40)
	putU32(raw, macVMExternalOffset, 150)
	putU32(raw, macVMInternalOffset, 220)
	// The 64-bit fields between the 32-bit ones must not leak into them.
	putU64(raw, 16, 0xffffffffffffffff)
	putU64(raw, 80, 0xffffffffffffffff)
	putU64(raw, 144, 0xffffffffffffffff)
	vm, ok := macDecodeVM(raw)
	if !ok || vm != (macVMStats{Free: 100, Active: 200, Inactive: 300, Wired: 50, Purgeable: 20, Speculative: 10, Compressor: 40, External: 150, Internal: 220}) {
		t.Fatalf("vm %+v ok=%v", vm, ok)
	}
	if _, ok := macDecodeVM(raw[:macVMMinimumSize-1]); ok {
		t.Fatal("a short buffer decoded")
	}
	mem := macMemory(vm, page, 1000*page)
	// app = 220-20, used = (200+50+40) pages, cache = 150+20 pages.
	if mem.UsedBytes != 290*page || mem.AvailableBytes != 710*page || mem.CacheBytes != 170*page || mem.TotalBytes != 1000*page || mem.SharedBytes != 0 {
		t.Fatalf("memory %+v", mem)
	}
	if over := macMemory(macVMStats{Internal: 5000}, page, 1000*page); over.UsedBytes != 1000*page || over.AvailableBytes != 0 {
		t.Fatalf("used past total was not clamped: %+v", over)
	}
}

func TestMacSwapLoadAndPressureLevel(t *testing.T) {
	swap := make([]byte, 32)
	putU64(swap, 0, 8<<30)
	putU64(swap, 8, 6<<30)
	putU64(swap, 16, 2<<30)
	if total, used, ok := macDecodeSwap(swap); !ok || total != 8<<30 || used != 2<<30 {
		t.Fatalf("swap %d %d %v", total, used, ok)
	}
	if _, _, ok := macDecodeSwap(swap[:24]); ok {
		t.Fatal("short xsw_usage decoded")
	}
	load := make([]byte, 24)
	putU32(load, 0, 2048)
	putU32(load, 4, 1024)
	putU32(load, 8, 512)
	putU64(load, 16, 2048)
	if got, ok := macDecodeLoad(load); !ok || got != [3]float64{1, 0.5, 0.25} {
		t.Fatalf("load %v %v", got, ok)
	}
	putU64(load, 16, 0)
	if _, ok := macDecodeLoad(load); ok {
		t.Fatal("zero load scale accepted")
	}
	for level, want := range map[uint32]string{1: "normal", 2: "warn", 4: "critical"} {
		if got, ok := macPressureLevel(level); !ok || got != want {
			t.Fatalf("level %d = %q %v", level, got, ok)
		}
	}
	for _, level := range []uint32{0, 3, 8} {
		if got, ok := macPressureLevel(level); ok || got != "" {
			t.Fatalf("level %d must be unknown, got %q", level, got)
		}
	}
}

func TestMacProcessDecodersAndTimebase(t *testing.T) {
	task := make([]byte, macTaskInfoSize)
	putU64(task, 8, 123456)
	putU64(task, 16, 30)
	putU64(task, 24, 12)
	if got, ok := macDecodeTaskInfo(task); !ok || got != (macTaskInfo{Resident: 123456, UserTime: 30, SystemTime: 12}) {
		t.Fatalf("task %+v", got)
	}
	bsd := make([]byte, macBSDInfoSize)
	putU32(bsd, 12, 4242)
	putU32(bsd, 16, 1)
	putU32(bsd, 20, 501)
	if pid, uid, ok := macDecodeBSDUID(bsd); !ok || pid != 4242 || uid != 501 {
		t.Fatalf("bsd %d %d %v", pid, uid, ok)
	}
	if _, _, ok := macDecodeBSDUID(bsd[:macBSDInfoSize-1]); ok {
		t.Fatal("short proc_bsdinfo decoded")
	}
	// Intel counts nanoseconds; Apple silicon counts 24 MHz ticks (125/3).
	if got := machToTicks(2_000_000_000, 1, 1); got != 200 {
		t.Fatalf("intel ticks %d", got)
	}
	if got := machToTicks(24_000_000, 125, 3); got != 100 {
		t.Fatalf("apple silicon ticks %d, want one second", got)
	}
	if got := machToTicks(1<<62, 125, 3); got == 0 {
		t.Fatal("a large counter wrapped to zero")
	}
	if machToTicks(5, 1, 0) != 0 {
		t.Fatal("zero denominator")
	}
}

func TestMacConsumersListOwnProcessesByExecutableNameWithoutArguments(t *testing.T) {
	uid := os.Getuid()
	records := map[int]workerproc.Process{
		10: {PID: 10, PPID: 1, Start: "Thu Oct 15 14:07:42 2026", RSS: 300 << 20, Args: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper (Renderer) --type=renderer --secret-token=abc"},
		11: {PID: 11, PPID: 1, Start: "Thu Oct 15 14:07:43 2026", RSS: 100 << 20, Args: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper (Renderer) --type=renderer --other"},
		20: {PID: 20, PPID: 1, Start: "Thu Oct 15 14:07:44 2026", RSS: 900 << 20, Args: "/usr/libexec/rootd"},
		30: {PID: 30, PPID: 1, Start: "Thu Oct 15 14:07:45 2026", RSS: 50 << 20, Args: "/Users/u/bin/tool --flag"},
	}
	info := map[int]macProcInfo{
		10: {UID: uid, Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper (Renderer)", Ticks: 1000},
		11: {UID: uid, Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper (Renderer)", Ticks: 500},
		20: {UID: 0, Ticks: 9},
	}
	lookup := func(pid int) (macProcInfo, bool) { i, ok := info[pid]; return i, ok }
	enriched := macEnrichProcesses(records, lookup)
	if records[10].UID != 0 || records[10].Ticks != 0 || records[10].Args == enriched[10].Args {
		t.Fatal("workerproc's own records were modified")
	}
	if enriched[10].Start != records[10].Start {
		t.Fatal("the process identity token changed")
	}
	if enriched[30].UID != -1 {
		t.Fatalf("a process libproc would not describe was kept: %+v", enriched[30])
	}

	first := processConsumers(enriched, nil, 5)
	if len(first) != 1 {
		t.Fatalf("consumers %+v, want the one own-user group", first)
	}
	group := first[0]
	if group.ID != "user:Google Chrome Helper (Renderer)" || group.ProcessCount != 2 || group.RSSBytes != 400<<20 || group.CPUPercent != nil {
		t.Fatalf("group %+v", group)
	}
	if len(group.Kinds) != 1 || group.Kinds[0] != "chrome" {
		t.Fatalf("kinds %v", group.Kinds)
	}
	for _, text := range append(append([]string{}, group.Commands...), group.ID) {
		if strings.Contains(text, "--") || strings.Contains(text, "secret") {
			t.Fatalf("arguments leaked into %q", text)
		}
	}
	// 5 s later the two processes used 300 and 150 more ticks: 450/5 = 90% of a core.
	info[10] = macProcInfo{UID: uid, Path: info[10].Path, Ticks: 1300}
	info[11] = macProcInfo{UID: uid, Path: info[11].Path, Ticks: 650}
	second := processConsumers(macEnrichProcesses(records, lookup), enriched, 5)
	if len(second) != 1 || second[0].CPUPercent == nil || *second[0].CPUPercent != 90 {
		t.Fatalf("cpu %+v", second)
	}
}

func TestMacProcessLabelFallsBackToTheFirstWordOnly(t *testing.T) {
	if got := macProcessLabel("", "/usr/bin/python3 server.py --token x"); got != "python3" {
		t.Fatalf("label %q", got)
	}
	if got := macProcessLabel("", "  "); got != "unknown" {
		t.Fatalf("label %q", got)
	}
}

func TestMacDisksListTheStartupContainerOnce(t *testing.T) {
	const block = 4096
	mounts := []macMount{
		{Path: "/System/Volumes/Data", FSType: "apfs", Device: "/dev/disk3s5", Local: true, Hidden: true, Blocks: 1000, Free: 400, Avail: 380, BlockSize: block},
		{Path: "/", FSType: "apfs", Device: "/dev/disk3s1s1", Local: true, Blocks: 1000, Free: 400, Avail: 380, BlockSize: block},
		{Path: "/Volumes/Backup", FSType: "apfs", Device: "/dev/disk3s6", Local: true, Blocks: 1000, Free: 400, Avail: 380, BlockSize: block},
		{Path: "/Volumes/Photos", FSType: "exfat", Device: "/dev/disk5s2", Local: true, Blocks: 500, Free: 100, Avail: 100, BlockSize: block},
		{Path: "/Volumes/Share", FSType: "smbfs", Device: "//u@nas/share", Blocks: 900, Free: 900, Avail: 900, BlockSize: block},
		{Path: "/dev", FSType: "devfs", Device: "devfs"},
	}
	disks := macDisks(mounts)
	if len(disks) != 2 || disks[0].Mount != "/" || disks[1].Mount != "/Volumes/Photos" {
		t.Fatalf("disks %+v", disks)
	}
	if disks[0].TotalBytes != 1000*block || disks[0].FreeBytes != 380*block || disks[0].UsedBytes != 600*block {
		t.Fatalf("startup volume %+v", disks[0])
	}
	if len(macDisks(nil)) != 0 {
		t.Fatal("no mounts must list no disks")
	}
}

func macFixtureReadings(cpuTotal, cpuIdle uint64, read, write uint64, ticks uint64) macReadings {
	load := [3]float64{1.5, 1, 0.5}
	vm := macVMStats{Wired: 100, Internal: 300, Compressor: 100, External: 200}
	io := ioCounters{Read: read, Write: write}
	uid := os.Getuid()
	return macReadings{
		CPU:  []cpuCounters{{Total: cpuTotal, Idle: cpuIdle}, {Total: cpuTotal, Idle: cpuTotal}},
		Load: &load, VM: &vm, PageSize: 16384, TotalMem: 1000 * 16384,
		SwapOK: true, SwapTotal: 4 << 30, SwapUsed: 1 << 30, Pressure: "warn",
		Mounts: []macMount{{Path: "/", FSType: "apfs", Device: "/dev/disk3s1s1", Local: true, Blocks: 1000, Free: 500, Avail: 480, BlockSize: 4096}},
		DiskIO: &io,
		Processes: macEnrichProcesses(
			map[int]workerproc.Process{7: {PID: 7, PPID: 1, Start: "s", RSS: 1 << 20, Args: "x"}},
			func(int) (macProcInfo, bool) {
				return macProcInfo{UID: uid, Path: "/usr/bin/node", Ticks: ticks}, true
			}),
	}
}

func TestMacSnapshotNeedsTwoIntervalsForRatesAndNeverFakesPSI(t *testing.T) {
	sampler := NewResourceSampler()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	first := sampler.buildMacSnapshot(t0, macFixtureReadings(1000, 800, 1<<20, 1<<20, 100))
	if first.CPU == nil || first.CPU.UtilizationPercent != nil || len(first.CPU.PerCorePercent) != 0 {
		t.Fatalf("first CPU %+v", first.CPU)
	}
	if first.Disks[0].ReadBytesPerSecond != nil || first.Consumers[0].CPUPercent != nil {
		t.Fatal("a rate appeared before one interval had passed")
	}
	if first.PSI != nil || first.MemoryPressure == nil || first.MemoryPressure.Level != "warn" {
		t.Fatalf("pressure model: psi=%v memory=%+v", first.PSI, first.MemoryPressure)
	}
	want := []string{"psi", "memory.shared"}
	if strings.Join(first.Unavailable, ",") != strings.Join(want, ",") {
		t.Fatalf("unavailable %v, want %v", first.Unavailable, want)
	}
	if first.Memory.UsedBytes != 500*16384 || first.Memory.SwapUsedBytes != 1<<30 || first.CPU.Load1 != 1.5 {
		t.Fatalf("memory %+v cpu %+v", first.Memory, first.CPU)
	}

	// Sample records previousAt after each pass; do the same here.
	sampler.previousAt = t0
	second := sampler.buildMacSnapshot(t0.Add(5*time.Second), macFixtureReadings(1200, 900, 1<<20+5<<20, 1<<20+10<<20, 150))
	// cpu0: 200 ticks total, 100 idle -> 50%; cpu1: all idle -> 0%; machine 400 total, 300 idle -> 25%.
	if second.CPU.UtilizationPercent == nil || *second.CPU.UtilizationPercent != 25 {
		t.Fatalf("utilization %v", second.CPU.UtilizationPercent)
	}
	if got := second.CPU.PerCorePercent; len(got) != 2 || got[0] != 50 || got[1] != 0 {
		t.Fatalf("per core %v", got)
	}
	disk := second.Disks[0]
	if disk.ReadBytesPerSecond == nil || *disk.ReadBytesPerSecond != float64(1<<20) || *disk.WriteBytesPerSecond != float64(2<<20) {
		t.Fatalf("disk rates %+v", disk)
	}
	if cpu := second.Consumers[0].CPUPercent; cpu == nil || *cpu != 10 {
		t.Fatalf("consumer cpu %v, want 50 ticks over 5 s = 10%%", cpu)
	}
}

func TestMacSnapshotReportsEveryFailedSourceUnavailable(t *testing.T) {
	sampler := NewResourceSampler()
	snap := sampler.buildMacSnapshot(time.Now(), macReadings{})
	if snap.CPU != nil || snap.Memory != nil || snap.MemoryPressure != nil || len(snap.Disks) != 0 || len(snap.Consumers) != 0 {
		t.Fatalf("empty reading produced data: %+v", snap)
	}
	for _, name := range []string{"psi", "cpu", "memory", "memory_pressure", "disks", "disks.io", "processes"} {
		if !containsString(snap.Unavailable, name) {
			t.Fatalf("%q missing from %v", name, snap.Unavailable)
		}
	}
}

func TestMemoryPressureLevelDrivesAlertsWhenAvailableMemoryIsLow(t *testing.T) {
	config := DefaultResourceThresholds()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	// A healthy 16 GiB Mac: Activity-Monitor-style availability is only 5%.
	lowAvailable := &MemoryResource{TotalBytes: 100, AvailableBytes: 5, UsedBytes: 95}
	healthy := MachineResourceSnapshot{Memory: lowAvailable, MemoryPressure: &MemoryPressure{Level: "normal"}}
	machine := NewPressureMachine(config)
	for i := 0; i < 20; i++ {
		if state, signals, notify := machine.Step(healthy, t0.Add(time.Duration(i)*5*time.Second)); state != "normal" || notify || len(signals) != 0 {
			t.Fatalf("step %d: low availability under normal kernel pressure alerted: %s %+v", i, state, signals)
		}
	}
	// Without the kernel's verdict (Linux) the same memory still alerts.
	linux := MachineResourceSnapshot{Memory: lowAvailable}
	linuxMachine := NewPressureMachine(config)
	if _, signals, _ := linuxMachine.Step(linux, t0); len(signals) != 1 || signals[0].Name != "memory.available_percent" || signals[0].State != "critical" {
		t.Fatalf("Linux availability signal changed: %+v", signals)
	}

	for _, tc := range []struct {
		level, want string
		value       float64
	}{{"warn", "elevated", 2}, {"critical", "critical", 4}} {
		machine := NewPressureMachine(config)
		snap := MachineResourceSnapshot{Memory: lowAvailable, MemoryPressure: &MemoryPressure{Level: tc.level}}
		var state string
		var notify, notified bool
		var signals []PressureSignal
		for i := 0; i <= 5; i++ {
			state, signals, notify = machine.Step(snap, t0.Add(time.Duration(i)*5*time.Second))
			notified = notified || notify
		}
		if state != tc.want || !notified || len(signals) != 1 || signals[0].Name != "memory.pressure_level" || signals[0].Value != tc.value || signals[0].State != tc.want {
			t.Fatalf("%s: state=%s notified=%v signals=%+v", tc.level, state, notified, signals)
		}
		// Recovery needs the kernel to say normal for the recovery period.
		snap.MemoryPressure.Level = "normal"
		recovered := false
		for i := 6; i < 20 && !recovered; i++ {
			state, _, recovered = machine.Step(snap, t0.Add(time.Duration(i)*5*time.Second))
		}
		if !recovered || state != "normal" {
			t.Fatalf("%s: no recovery event, state %s", tc.level, state)
		}
	}
}

func TestMemoryPressureAloneCountsAsMeasurement(t *testing.T) {
	// A snapshot whose only data is the kernel verdict is still evidence.
	machine := NewPressureMachine(DefaultResourceThresholds())
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	machine.Step(MachineResourceSnapshot{MemoryPressure: &MemoryPressure{Level: "critical"}}, t0)
	state, _, notify := machine.Step(MachineResourceSnapshot{MemoryPressure: &MemoryPressure{Level: "critical"}}, t0.Add(30*time.Second))
	if state != "critical" || !notify {
		t.Fatalf("state %s notify %v", state, notify)
	}
}

func TestLinuxShapedSnapshotCarriesNoMacOSKeys(t *testing.T) {
	avg := &PressureAverages{}
	snap := MachineResourceSnapshot{
		Version: 2, SampledAt: time.Unix(0, 0).UTC(), State: "normal",
		CPU: &CPUResource{}, Memory: &MemoryResource{TotalBytes: 1},
		PSI:   &PressureSnapshot{CPU: PressureResource{Some: avg}},
		Disks: []DiskResource{}, Consumers: []ProcessConsumer{}, Signals: []PressureSignal{},
		History: []TelemetryHistoryPoint{{SampledAt: time.Unix(0, 0).UTC(), State: "normal"}},
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(ResourcePressureEvent{State: "elevated"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(raw), string(event)} {
		if strings.Contains(text, "memory_pressure") {
			t.Fatalf("a Linux-shaped payload gained a macOS key: %s", text)
		}
	}
}

func TestMacSnapshotJSONContract(t *testing.T) {
	sampler := NewResourceSampler()
	snap := sampler.buildMacSnapshot(time.Now(), macFixtureReadings(1000, 800, 1, 1, 1))
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["psi"]; ok {
		t.Fatalf("a Mac payload carries a psi object: %s", raw)
	}
	var pressure struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal(payload["memory_pressure"], &pressure); err != nil || pressure.Level != "warn" {
		t.Fatalf("memory_pressure %s: %v", payload["memory_pressure"], err)
	}
	var unavailable []string
	if err := json.Unmarshal(payload["unavailable"], &unavailable); err != nil || len(unavailable) != 2 {
		t.Fatalf("unavailable %s: %v", payload["unavailable"], err)
	}
}
