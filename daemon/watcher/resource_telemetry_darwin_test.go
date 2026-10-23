//go:build darwin

package watcher

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// These call the real macOS APIs, so they prove the struct layouts and
// constants in resource_macos.go on the machine that matters.

func TestDarwinNativeSamplesAreCoherent(t *testing.T) {
	lib, err := loadMacNative()
	if err != nil {
		t.Fatal(err)
	}
	if cpus := lib.cpuLoad(); len(cpus) != runtime.NumCPU() || cpus[0].Total == 0 {
		t.Fatalf("host_processor_info returned %d CPUs for %d", len(cpus), runtime.NumCPU())
	}
	vm, ok := lib.vmStats()
	if !ok {
		t.Fatal("host_statistics64 failed")
	}
	pages := vm.Free + vm.Active + vm.Inactive + vm.Wired
	if pages == 0 || vm.Internal+vm.External == 0 {
		t.Fatalf("vm stats %+v", vm)
	}
	if lib.pageSize != 4096 && lib.pageSize != 16384 {
		t.Fatalf("page size %d", lib.pageSize)
	}
	if lib.numer == 0 || lib.denom == 0 {
		t.Fatalf("timebase %d/%d", lib.numer, lib.denom)
	}
}

func TestDarwinProcInfoDescribesThisProcess(t *testing.T) {
	lib, err := loadMacNative()
	if err != nil {
		t.Fatal(err)
	}
	// Spend CPU so the process has measurable time.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	info, ok := lib.procInfo(os.Getpid(), os.Getuid(), make([]byte, macTaskAllInfoSize), make([]byte, macProcPathMax))
	if !ok || info.UID != os.Getuid() || info.Ticks == 0 {
		t.Fatalf("proc info %+v ok=%v", info, ok)
	}
	self, _ := os.Executable()
	if filepath.Base(info.Path) != filepath.Base(self) {
		t.Fatalf("path %q, executable %q", info.Path, self)
	}
}

func TestDarwinSamplePlatformReportsRealData(t *testing.T) {
	sampler := NewResourceSampler()
	now := time.Now()
	sampler.samplePlatform(now)
	sampler.previousAt = now
	snap := sampler.samplePlatform(now.Add(2 * time.Second))
	if snap.Memory == nil || snap.Memory.TotalBytes == 0 || snap.CPU == nil || len(snap.Disks) == 0 || len(snap.Consumers) == 0 {
		t.Fatalf("snapshot %+v", snap)
	}
	if snap.PSI != nil {
		t.Fatal("macOS must not report PSI")
	}
}
