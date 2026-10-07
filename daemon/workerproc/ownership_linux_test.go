package workerproc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnershipFencesReusedPIDAndFollowsDetachedChildren(t *testing.T) {
	records := map[int]Process{10: {PID: 10, ResourceID: "owned", Start: "a"}, 11: {PID: 11, PPID: 10, Start: "b"}, 20: {PID: 20, PPID: 1, ResourceID: "owned", Start: "c"}, 21: {PID: 21, PPID: 20, Start: "d"}, 30: {PID: 30, Start: "new"}, 40: {PID: 40, Start: "persisted"}}
	lease := Lease{ResourceID: "owned", Observed: []ProcessIdentity{{30, "old"}, {40, "persisted"}}}
	got := Owned(records, lease)
	if len(got) != 5 || got[30].PID != 0 || got[21].PID != 21 || got[40].PID != 40 {
		t.Fatal(got)
	}
	if got = Owned(records, Lease{ResourceID: "owned", BootID: "previous-boot"}); len(got) != 0 {
		t.Fatal("old boot adopted", got)
	}
}
func TestProcStatHandlesSpacesParenthesesAndZombie(t *testing.T) {
	raw := "123 (qemu (tool)) S 1 123 123 0 0 0 0 0 0 0 200 50 0 0 0 0 0 0 900 0 17 0"
	p, err := ParseProcStat(raw)
	if err != nil || p.Args != "qemu (tool)" || p.Start != "900" || p.Ticks != 250 || p.RSS != 17*uint64(os.Getpagesize()) {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := ParseProcStat(strings.Replace(raw, " S ", " Z ", 1)); err == nil {
		t.Fatal("zombie accepted")
	}
}
func TestStopOwnershipReclaimsDetachedToolAndPreservesForeignProcess(t *testing.T) {
	token := fmt.Sprintf("zen-test-%d", time.Now().UnixNano())
	dir := t.TempDir()
	path, _ := LeasePath(dir, token)
	if err := WriteOwnershipLease(path, token); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "pid")
	tool := exec.Command("setsid", "sh", "-c", "echo $$ > \"$1\"; exec sleep 90", "sh", marker)
	tool.Env = append(os.Environ(), "MEWLA_WORKER_RESOURCE_UNIT="+token)
	if err := tool.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tool.Process.Kill(); _ = tool.Wait() }()
	foreign := exec.Command("sleep", "90")
	foreign.Env = []string{"PATH=/usr/bin:/bin"}
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = foreign.Process.Kill(); _ = foreign.Wait() }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tool did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := ObserveLeases(dir); err != nil {
		t.Fatal(err)
	}
	if err := StopLease(path); err != nil {
		t.Fatal(err)
	}
	records, err := Processes(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := records[tool.Process.Pid]; ok {
		t.Fatal("owned detached tool remains")
	}
	if _, ok := records[foreign.Process.Pid]; !ok {
		t.Fatal("foreign process was killed")
	}
	t.Logf("reclaimed owned detached pid=%d; foreign pid=%d remains", tool.Process.Pid, foreign.Process.Pid)
}
