package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Opt-in machine acceptance: runs at the production cadence, includes the
// process ownership walk, and checks CPU cost and absence of sample writes.
func TestResourceSamplerLiveCost(t *testing.T) {
	if os.Getenv("MEWLA_VERIFY_RESOURCE_COST") != "1" {
		t.Skip("opt-in live sampler cost")
	}
	s := NewResourceSampler()
	s.ConfigureResourceFiles(t.TempDir())
	// Hold the transition candidate beyond this measurement; transition
	// persistence is intentional and is tested separately.
	s.machine.Config.SustainSeconds = 600
	readIO := func() map[string]uint64 {
		raw, err := os.ReadFile("/proc/self/io")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]uint64{}
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 {
				out[strings.TrimSuffix(f[0], ":")], _ = strconv.ParseUint(f[1], 10, 64)
			}
		}
		return out
	}
	cpu := func() float64 {
		var r syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
			t.Fatal(err)
		}
		return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
	}
	var totalCPU, totalWall float64
	var rchar, wchar, reads, writes uint64
	const count = 6
	for i := 0; i < count; i++ {
		if i > 0 {
			time.Sleep(5 * time.Second)
		}
		before := readIO()
		c := cpu()
		start := time.Now()
		s.Sample(nil, start)
		totalWall += time.Since(start).Seconds()
		totalCPU += cpu() - c
		after := readIO()
		rchar += after["rchar"] - before["rchar"]
		wchar += after["wchar"] - before["wchar"]
		reads += after["read_bytes"] - before["read_bytes"]
		writes += after["write_bytes"] - before["write_bytes"]
	}
	t.Logf("%d ticks: CPU %.3f ms/tick, wall %.3f ms/tick, average %.3f%% core at 5 s; rchar %d B/tick, wchar %d, disk read %d, disk write %d total", count, totalCPU*1000/count, totalWall*1000/count, totalCPU/count/5*100, rchar/count, wchar, reads, writes)
	if totalCPU/count > .05 {
		t.Fatal("sampler exceeds 1% of one core at 5 s cadence")
	}
	if wchar != 0 || writes != 0 {
		t.Fatal("sampling wrote data")
	}
}

func TestResourceConfigurationRecoveryAndHistory(t *testing.T) {
	dir := t.TempDir()
	s := NewResourceSampler()
	s.ConfigureResourceFiles(dir)
	cfg := DefaultResourceThresholds()
	cfg.SustainSeconds = 45
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "resource-telemetry.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s.Sample(nil, time.Now())
	if s.machine.Config.SustainSeconds != 45 {
		t.Fatal("config not reloaded")
	}
	if err := os.Remove(filepath.Join(dir, "resource-telemetry.json")); err != nil {
		t.Fatal(err)
	}
	// The test advances the sample timestamp without waiting for a real kernel
	// tick. Seed the CPU baseline so two immediate /proc reads cannot produce
	// an unavailable delta merely because the counters have not advanced.
	s.previousCPU = map[string]cpuCounters{"cpu": {}}
	snap, _ := s.Sample(nil, time.Now().Add(5*time.Second))
	if s.machine.Config.SustainSeconds != 20 {
		t.Fatal("defaults not restored")
	}
	points := s.Snapshot().History
	if len(points) != 2 {
		t.Fatal(points)
	}
	p := points[1]
	if p.CPUPercent == nil || p.MemoryUsedBytes == nil || p.SwapUsedBytes == nil {
		t.Fatal("chart history fields missing")
	}
	// PSI depends on the host kernel. History must preserve every available
	// metric and leave unsupported metrics absent instead of inventing zeroes.
	for _, metric := range []struct {
		name    string
		history *float64
		sampled *PressureAverages
	}{
		{"cpu", p.PSICPUSomeAvg10, snap.PSI.CPU.Some},
		{"memory", p.PSIMemorySomeAvg10, snap.PSI.Memory.Some},
		{"io", p.PSIIOSomeAvg10, snap.PSI.IO.Some},
	} {
		if metric.sampled == nil {
			if metric.history != nil {
				t.Fatalf("history invented unavailable %s PSI", metric.name)
			}
		} else if metric.history == nil || *metric.history != metric.sampled.Avg10 {
			t.Fatalf("history did not preserve %s PSI", metric.name)
		}
	}
	at := time.Now().UTC()
	s.pending = []ResourcePressureEvent{{SampledAt: at, State: "elevated"}}
	s.machine.State = "elevated"
	if err := s.saveResourceState(); err != nil {
		t.Fatal(err)
	}
	next := NewResourceSampler()
	next.ConfigureResourceFiles(dir)
	if next.machine.State != "elevated" || len(next.PendingResourceEvents()) != 1 {
		t.Fatal("pending transition lost on restart")
	}
	if err := next.AcknowledgeResourceEvent(at); err != nil {
		t.Fatal(err)
	}
	again := NewResourceSampler()
	again.ConfigureResourceFiles(dir)
	if len(again.PendingResourceEvents()) != 0 {
		t.Fatal("acknowledged transition replayed")
	}
}
