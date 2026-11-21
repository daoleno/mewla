//go:build linux

package watcher

import (
	"bufio"
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

// startRunningCat starts `cat -u` and returns once cat itself has echoed a
// line, so its exec is over and /proc shows its own comm and command line.
// cmd.Start returns earlier: a /proc read right after it can still show the
// forked copy of this test binary (its comm and arguments), or "cat" with an
// empty command line while exec lays the new program out.
func startRunningCat(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("cat", "-u")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if _, err := stdin.Write([]byte("ready\n")); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("cat echoed %q (%v)", line, err)
	}
	return cmd
}

func TestSnapshotProcessTableSeesARealProcess(t *testing.T) {
	cmd := startRunningCat(t)
	processes, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := processes[cmd.Process.Pid]
	if !ok {
		t.Fatalf("spawned pid %d missing from the table", cmd.Process.Pid)
	}
	if got.ppid != os.Getpid() || got.comm != "cat" || got.args != "cat -u" {
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
	cmd := startRunningCat(t)
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
	if first[pid].args != "cat -u" || second[pid].args != "cat -u" {
		t.Fatalf("args %q then %q", first[pid].args, second[pid].args)
	}
}

// A shell forked from a script carries the script's text until it execs; when
// the exec keeps the name (sh to sh) a reading cached before it must not stand
// for cmdlineRefresh. This was the flake of TestStopTurnCommandsEndsOnlyTheGroupStartedThisTurn
// (11-21: three CI runs): the Stop test's wait for a child named "count" matched
// the earlier command's shell by the script text it was forked with.
func TestSnapshotProcessTableRereadsAYoungProcessThatExecsUnderTheSameName(t *testing.T) {
	cmd := exec.Command("sh", "-c", `m=AF; n=TER; sleep 0.5; exec sh -c "echo $m$n; sleep 30; echo end"`)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	first, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	if args := first[pid].args; !strings.Contains(args, "m=AF") {
		t.Fatalf("before the exec: args %q", args)
	}
	time.Sleep(900 * time.Millisecond)
	second, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	if args := second[pid].args; !strings.Contains(args, "echo AFTER") {
		t.Fatalf("after the exec (same pid, same name): args %q, want the new command line", args)
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
	pane := startRunningCat(t)
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
	if got := table[pane.Process.Pid]; got.comm != "cat" || got.args != "cat -u" {
		t.Fatalf("pane process = %+v", got)
	}

	child := startRunningCat(t)
	table, err = snapshotProcessTableHot(roots, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := table[child.Process.Pid]; !ok || got.args != "cat -u" {
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
