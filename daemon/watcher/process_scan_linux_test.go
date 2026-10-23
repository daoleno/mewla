//go:build linux

package watcher

import (
	"os"
	"os/exec"
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
	cached := procSnapshots.cmdlines[pid]
	cached.readAt = time.Now().Add(-2 * cmdlineRefresh)
	cached.args = "stale"
	procSnapshots.cmdlines[pid] = cached
	second, err := snapshotProcessTable()
	if err != nil {
		t.Fatal(err)
	}
	if first[pid].args != "sleep 30" || second[pid].args != "sleep 30" {
		t.Fatalf("args %q then %q", first[pid].args, second[pid].args)
	}
}
