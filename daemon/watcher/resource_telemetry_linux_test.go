package watcher

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/workerproc"
)

func TestResourceParsers(t *testing.T) {
	psi := ParsePSI("some avg10=7.7 avg60=4.2 avg300=1.1 total=99\nfull avg10=2.3 avg60=1.2 avg300=0.4 total=5\n")
	if psi.Some == nil || psi.Some.Avg10 != 7.7 || psi.Some.Avg60 != 4.2 || psi.Some.Avg300 != 1.1 || psi.Full.Avg10 != 2.3 {
		t.Fatalf("PSI %+v", psi)
	}
	if ParsePSI("some avg10=broken avg60=1 avg300=2").Some != nil {
		t.Fatal("invalid PSI accepted")
	}
	mem := parseMemoryInfo("MemTotal: 1000 kB\nMemAvailable: 400 kB\nCached: 100 kB\nSReclaimable: 20 kB\nShmem: 50 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	if mem.UsedBytes != 600*1024 || mem.CacheBytes != 120*1024 || mem.SharedBytes != 50*1024 || mem.SwapUsedBytes != 0 {
		t.Fatalf("memory %+v", mem)
	}
	first := parseCPUStat("cpu 100 0 50 800 50 0 0 0 99 0\ncpu0 10 0 5 80 5 0 0 0\n")
	second := parseCPUStat("cpu 150 0 50 840 60 0 0 0 999 0\n")
	if percent, ok := cpuDelta(first["cpu"], second["cpu"]); !ok || percent != 50 {
		t.Fatalf("CPU delta %v %v", percent, ok)
	}
	if _, ok := cpuDelta(second["cpu"], first["cpu"]); ok {
		t.Fatal("counter reset accepted")
	}
	io := parseDiskStats("259 1 nvme0n1p1 1 0 20 0 2 0 40 0 0 0 0\n")
	if io["259:1"].Read != 10240 || io["259:1"].Write != 20480 {
		t.Fatal(io)
	}
}
func TestResourceAttributionIncludesDetachedWorkerBrainDockerAndUser(t *testing.T) {
	uid := os.Getuid()
	records := map[int]workerproc.Process{
		10: {PID: 10, UID: uid, ResourceID: "token", WorkerID: "w", Start: "1", RSS: 100, Args: "codex", Ticks: 100},
		11: {PID: 11, UID: uid, PPID: 10, Start: "2", RSS: 500, Args: "qemu-system-x86", Ticks: 150},
		20: {PID: 20, UID: uid, WorkerID: "host", Start: "3", RSS: 80, Args: "claude"},
		30: {PID: 30, UID: uid, ContainerID: "container1", Start: "4", RSS: 50, Args: "node"},
		40: {PID: 40, UID: uid, Start: "5", RSS: 40, Args: "chrome"},
	}
	previous := map[int]workerproc.Process{10: {Start: "1", Ticks: 50}, 11: {Start: "old-reused", Ticks: 100}}
	groups := processConsumers(records, previous, 5)
	w := New(time.Second)
	w.workers["w"] = &classifier.Worker{ID: "w", Name: "Worker", Delegated: true, State: classifier.StateRunning}
	w.workers["host"] = &classifier.Worker{ID: "host", Name: "Brain", Hidden: true, State: classifier.StateDone}
	w.workerOrder = []string{"w", "host"}
	out := attributeConsumers(groups, w)
	owners := map[string]ProcessConsumer{}
	for _, c := range out {
		owners[c.Owner] = c
	}
	if owners["worker"].RSSBytes != 600 || owners["worker"].CPUPercent == nil || *owners["worker"].CPUPercent != 10 || !containsString(owners["worker"].Kinds, "qemu") {
		t.Fatalf("worker %+v", owners["worker"])
	}
	if owners["brain"].WorkerID != "host" || owners["docker"].RSSBytes != 50 || owners["user"].RSSBytes != 40 {
		t.Fatalf("owners %+v", owners)
	}
}
func TestResourceJSONUsesDistinctContractFields(t *testing.T) {
	sampler := NewResourceSampler()
	snap, _ := sampler.Sample(nil, time.Now())
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	memory := payload["memory"].(map[string]any)
	for _, key := range []string{"total_bytes", "used_bytes", "available_bytes", "cache_bytes", "shared_bytes", "swap_total_bytes", "swap_used_bytes"} {
		if _, ok := memory[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if payload["version"] != float64(2) || payload["endpoint"] != "get_resource_telemetry" {
		t.Fatal(payload)
	}
}

type unitOwningManager struct {
	noopDelegatedResourceManager
	owned string
}

func (m unitOwningManager) OwnsUnit(unit string) bool { return unit == m.owned }

// 11-23: two process trees carrying a Worker id were reported as orphaned
// Workers although this daemon started neither: another Mewla daemon's Brain
// host (an isolated test daemon left running) and Codex's shared app-server,
// which detached from the Worker that first started it. Only a tree holding
// this daemon's own resource lease, whose Session is gone, is an orphan.
func TestOnlyATreeThisDaemonStartedIsAnOrphanedWorker(t *testing.T) {
	uid := os.Getuid()
	ours := "mewla-worker-owner-0123456789abcdef0123456789abcdef.scope"
	records := map[int]workerproc.Process{
		10: {PID: 10, UID: uid, ResourceID: ours, WorkerID: "mh-gone", Start: "1", RSS: 100, Args: "claude"},
		20: {PID: 20, UID: uid, WorkerID: "mh-ea6cc5123c96fdef18bda153", Start: "2", RSS: 120, Args: "claude --permission-mode bypassPermissions"},
		30: {PID: 30, UID: uid, WorkerID: "mh-cf3b58ae58b819b6ddf47e09", Start: "3", RSS: 80, Args: "codex app-server daemon pid-update-loop"},
	}
	w := New(time.Second)
	w.resources = unitOwningManager{owned: ours}
	out := attributeConsumers(processConsumers(records, nil, 0), w)
	owners := map[int]string{}
	for _, c := range out {
		for _, p := range c.Processes {
			owners[p.PID] = c.Owner
			if c.Owner == "user" && c.WorkerID != "" {
				t.Errorf("pid %d: a tree this daemon did not start keeps the Worker id %q", p.PID, c.WorkerID)
			}
		}
	}
	if owners[10] != "orphaned_worker" || owners[20] != "user" || owners[30] != "user" {
		t.Fatalf("owners = %v", owners)
	}
}
