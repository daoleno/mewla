//go:build linux

package workerproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func selfReadSyscalls(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(line, "syscr: "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	t.Fatal("no syscr")
	return 0
}

// execDone waits until pid runs program with its environment laid out:
// exec.Cmd.Start can return before the child's exec or inside it.
func execDone(t *testing.T, pid int, program string) {
	t.Helper()
	root := "/proc/" + strconv.Itoa(pid)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		exe, _ := os.Readlink(root + "/exe")
		if raw, err := os.ReadFile(root + "/environ"); err == nil && len(raw) > 0 && filepath.Base(exe) == program {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d never finished its exec", pid)
		}
	}
}

// Every poll of the watcher observes which processes a Worker owns. Walking the
// whole machine for that read the stat of every process several times a
// second; only a process that is new can have changed what it is owned by.
func TestRecentProcessListRereadsOnlyNewProcesses(t *testing.T) {
	processCache.Lock()
	processCache.processes, processCache.at = nil, time.Time{}
	processCache.Unlock()

	before := selfReadSyscalls(t)
	first, err := Processes(true)
	if err != nil {
		t.Fatal(err)
	}
	full := selfReadSyscalls(t) - before
	if full < 200 {
		t.Skipf("only %d reads for a full walk: too few processes to compare", full)
	}

	owned := exec.Command("sleep", "60")
	owned.Env = append(os.Environ(), "MEWLA_WORKER_RESOURCE_UNIT=age-test-unit")
	if err := owned.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = owned.Process.Kill(); _, _ = owned.Process.Wait() }()
	execDone(t, owned.Process.Pid, "sleep")

	before = selfReadSyscalls(t)
	second, err := ProcessesRecent()
	if err != nil {
		t.Fatal(err)
	}
	again := selfReadSyscalls(t) - before
	if again*4 > full {
		t.Fatalf("a second recent list took %d read syscalls against %d for the first", again, full)
	}
	if got, ok := second[owned.Process.Pid]; !ok || got.ResourceID != "age-test-unit" {
		t.Fatalf("a process started since the last list is missing or unowned: %+v", got)
	}
	if len(second) < len(first)/2 {
		t.Fatalf("the list shrank from %d to %d processes", len(first), len(second))
	}

	// A record read inside its execve (the environment not laid out yet) is
	// read again, not kept unowned for the process's life.
	caught := second[owned.Process.Pid]
	caught.ResourceID, caught.envPending = "", true
	processCache.Lock()
	processCache.processes[owned.Process.Pid] = caught
	processCache.Unlock()
	third, err := ProcessesRecent()
	if err != nil {
		t.Fatal(err)
	}
	if got := third[owned.Process.Pid]; got.ResourceID != "age-test-unit" || got.envPending {
		t.Fatalf("a process read inside its exec stayed unowned: %+v", got)
	}
	// So is one read before its exec, as its parent's copy, once its record
	// is old enough to be read again.
	caught = third[owned.Process.Pid]
	caught.ResourceID, caught.Args = "", "workerproc.test"
	caught.readAt = time.Now().Add(-2 * ownershipRecordAge)
	processCache.Lock()
	processCache.processes[owned.Process.Pid] = caught
	processCache.Unlock()
	third, err = ProcessesRecent()
	if err != nil {
		t.Fatal(err)
	}
	if got := third[owned.Process.Pid]; got.ResourceID != "age-test-unit" {
		t.Fatalf("a process read before its exec kept its parent's owner: %+v", got)
	}

	// A record older than the age is read again.
	other := second[os.Getpid()]
	other.readAt = time.Now().Add(-2 * ownershipRecordAge)
	other.RSS = 1
	processCache.Lock()
	processCache.processes[os.Getpid()] = other
	processCache.Unlock()
	fourth, err := ProcessesRecent()
	if err != nil {
		t.Fatal(err)
	}
	if fourth[os.Getpid()].RSS == 1 {
		t.Fatal("an old record was kept")
	}

	// A list that decides whether a process is gone reads every process.
	// (Each process it lists took a read of its stat; the machine's processes
	// may have changed since the first list.)
	before = selfReadSyscalls(t)
	fresh, err := Processes(true)
	if err != nil {
		t.Fatal(err)
	}
	if exact := selfReadSyscalls(t) - before; exact < uint64(len(fresh)) {
		t.Fatalf("a fresh list of %d processes took %d read syscalls", len(fresh), exact)
	}
}
