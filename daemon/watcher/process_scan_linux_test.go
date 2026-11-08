//go:build linux

package watcher

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseProcStatLineKeepsOddNamesIntact(t *testing.T) {
	raw := []byte("42 (a (b) c) S 7 8 9 34816 10 4194304 1 0 0 0 0 0 0 0 20 0 1 0 12345 1000 10 18446744073709551615")
	info, start, ok := parseProcStatLine(raw)
	if !ok || info.comm != "a (b) c" || info.ppid != 7 || info.pgid != 8 || info.tpgid != 10 || start != 12345 {
		t.Fatalf("parsed %+v start=%d ok=%v", info, start, ok)
	}
	if _, _, ok := parseProcStatLine([]byte("42 (short) S 1")); ok {
		t.Fatal("a truncated stat line must not parse")
	}
}

func TestSnapshotProcessTableSeesARealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	processes, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := processes[cmd.Process.Pid]
	if !ok {
		t.Fatalf("spawned pid %d missing from the table", cmd.Process.Pid)
	}
	if got.ppid != os.Getpid() || got.comm != "sleep" || got.args != "sleep 30" {
		t.Fatalf("process = %+v", got)
	}
	if since := time.Since(got.startedAt); since < 0 || since > time.Minute {
		t.Fatalf("start %v is implausible (%v ago)", got.startedAt, since)
	}
	self := processes[os.Getpid()]
	if self.pid != os.Getpid() || !strings.Contains(self.args, "watcher") {
		t.Fatalf("own entry = %+v", self)
	}
}

func TestSnapshotProcessTableRereadsArgsAfterExecUnderTheSameName(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	first, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	cached := procSnapshots.procs[pid]
	cached.argsAt = time.Now().Add(-2 * cmdlineRefresh)
	cached.args = "stale"
	procSnapshots.procs[pid] = cached
	second, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	if first[pid].args != "sleep 30" || second[pid].args != "sleep 30" {
		t.Fatalf("args %q then %q", first[pid].args, second[pid].args)
	}
}

// selfReads is this process's read syscall count and bytes, from procfs.
func selfReads(t *testing.T) (syscr, rchar uint64) {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		switch name {
		case "syscr":
			syscr = n
		case "rchar":
			rchar = n
		}
	}
	return syscr, rchar
}

// The poll loop reads the process table up to twice a second while any pane
// prints. Outside the panes' own trees a process is read once per cold age, so
// a poll must cost a fraction of a full /proc walk, yet a new child under a
// pane must still show up in the very next poll.
func TestHotProcessScanRereadsOnlyThePaneTree(t *testing.T) {
	pane := exec.Command("sleep", "60")
	if err := pane.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = pane.Process.Kill(); _, _ = pane.Process.Wait() }()
	roots := []int{pane.Process.Pid}

	if _, err := snapshotProcessTableHot(roots, time.Hour); err != nil {
		t.Fatal(err)
	}
	full0, _ := selfReads(t)
	if _, err := snapshotProcessTable(); err != nil {
		t.Fatal(err)
	}
	full1, _ := selfReads(t)
	fullCost := full1 - full0

	hot0, _ := selfReads(t)
	table, err := snapshotProcessTableHot(roots, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hot1, _ := selfReads(t)
	hotCost := hot1 - hot0
	if fullCost < 200 {
		t.Skipf("only %d reads for a full walk: too few processes to compare", fullCost)
	}
	if hotCost*4 > fullCost {
		t.Fatalf("a hot scan took %d read syscalls against %d for a full walk", hotCost, fullCost)
	}
	if got := table[pane.Process.Pid]; got.comm != "sleep" || got.args != "sleep 60" {
		t.Fatalf("pane process = %+v", got)
	}

	child := exec.Command("sleep", "61")
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() }()
	table, err = snapshotProcessTableHot(roots, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := table[child.Process.Pid]; !ok || got.args != "sleep 61" {
		t.Fatalf("a process started since the last scan is missing or stale: %+v", got)
	}
}

func TestHotProcessScanFollowsAColdProcessAfterItsAge(t *testing.T) {
	other := exec.Command("sleep", "62")
	if err := other.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = other.Process.Kill(); _, _ = other.Process.Wait() }()
	pid := other.Process.Pid
	if _, err := snapshotProcessTableHot(nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	cached := procSnapshots.procs[pid]
	cached.info.pgid = -7
	cached.statAt = time.Now()
	procSnapshots.procs[pid] = cached
	table, _ := snapshotProcessTableHot(nil, time.Hour)
	if table[pid].pgid != -7 {
		t.Fatal("a young cold reading was reread")
	}
	cached.statAt = time.Now().Add(-2 * time.Hour)
	procSnapshots.procs[pid] = cached
	table, _ = snapshotProcessTableHot(nil, time.Hour)
	if table[pid].pgid == -7 {
		t.Fatal("an old cold reading was kept")
	}
}
