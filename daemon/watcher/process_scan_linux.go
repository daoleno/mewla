//go:build linux

package watcher

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// userHZ is the clock tick rate of /proc times. The kernel's user-space ABI
// fixes it at 100 on every Linux architecture, whatever CONFIG_HZ is.
const userHZ = 100

// cmdlineRefresh bounds how long a cached command line is trusted: exec keeps
// the pid and start time, so a process that re-execs under the same name
// changes its arguments without changing its identity.
const cmdlineRefresh = 10 * time.Second

// cachedProc is what one scan learned about a process. stat is the parsed
// /proc/<pid>/stat, args the command line; each carries the time it was read
// so a scan can leave an unchanged-looking process alone.
type cachedProc struct {
	start  int64
	info   processInfo
	args   string
	statAt time.Time
	argsAt time.Time
}

var procSnapshots struct {
	sync.Mutex
	procs map[int]cachedProc
	boot  int64
}

// snapshotProcessTable reads the whole process table from /proc in this
// process: no ps to fork and parse every few hundred milliseconds. Fields match
// the ps invocation it replaces: ids from stat, ucomm from the stat name, args
// from cmdline ("[name]" for a kernel thread, as ps shows it). Every process is
// read afresh.
func snapshotProcessTable() (map[int]processInfo, error) {
	return scanProcessTable(nil, 0)
}

// snapshotProcessTableNear is the table the poll loop reads: see
// snapshotProcessTableHot.
func snapshotProcessTableNear(roots []int) (map[int]processInfo, error) {
	return snapshotProcessTableHot(roots, processColdAge)
}

// snapshotProcessTableHot is the table the poll loop needs several times a
// second. The panes' own process trees (the roots and everything below them)
// are read afresh every call, and so is any process not seen before, so a
// foreground change or a new tool child shows up as quickly as with a full
// read. Every other process keeps its last reading until it is coldAge old:
// a machine with thousands of processes is walked once per coldAge instead of
// once per poll.
func snapshotProcessTableHot(roots []int, coldAge time.Duration) (map[int]processInfo, error) {
	return scanProcessTable(roots, coldAge)
}

// scanProcessTable lists /proc and reads the stat of each process that is hot,
// new or at least coldAge old (every process when coldAge is zero).
func scanProcessTable(roots []int, coldAge time.Duration) (map[int]processInfo, error) {
	procSnapshots.Lock()
	defer procSnapshots.Unlock()

	if procSnapshots.boot == 0 {
		boot, err := readBootTime()
		if err != nil {
			return nil, err
		}
		procSnapshots.boot = boot
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("proc process snapshot: %w", err)
	}
	now := time.Now()
	previous := procSnapshots.procs
	hot := hotProcesses(previous, roots)
	next := make(map[int]cachedProc, len(previous))
	processes := make(map[int]processInfo, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		cached, seen := previous[pid]
		if seen && !hot[pid] && coldAge > 0 && now.Sub(cached.statAt) < coldAge {
			next[pid] = cached
			info := cached.info
			info.args = cached.args
			processes[pid] = info
			continue
		}
		raw, err := workerproc.ReadProcFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue // exited since the directory listing
		}
		info, start, ok := parseProcStatLine(raw)
		if !ok {
			continue
		}
		info.pid = pid
		info.startedAt = time.Unix(procSnapshots.boot+start/userHZ, 0)

		statAt := now
		if !seen && coldAge > 0 {
			// Spread the cold re-reads of a first scan over the window
			// instead of repeating them all in the same poll.
			statAt = now.Add(-time.Duration(pid%64) * coldAge / 64)
		}
		if !seen || cached.start != start || cached.info.comm != info.comm || now.Sub(cached.argsAt) >= cmdlineRefresh {
			cached = cachedProc{start: start, args: readCmdline(entry.Name(), info.comm), argsAt: now}
		}
		cached.start, cached.info, cached.statAt = start, info, statAt
		next[pid] = cached
		info.args = cached.args
		processes[pid] = info
	}
	procSnapshots.procs = next
	if len(processes) == 0 {
		return nil, fmt.Errorf("proc process snapshot found no processes")
	}
	return processes, nil
}

// hotProcesses is the roots and every process below them, by the parent ids of
// the previous scan. A process that appeared since is not in that scan, so it
// is read afresh anyway.
func hotProcesses(previous map[int]cachedProc, roots []int) map[int]bool {
	if len(roots) == 0 || len(previous) == 0 {
		return nil
	}
	children := make(map[int][]int, len(previous))
	for pid, cached := range previous {
		children[cached.info.ppid] = append(children[cached.info.ppid], pid)
	}
	hot := make(map[int]bool, 4*len(roots))
	queue := append([]int(nil), roots...)
	for len(queue) > 0 {
		pid := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if hot[pid] {
			continue
		}
		hot[pid] = true
		queue = append(queue, children[pid]...)
	}
	return hot
}

// parseProcStatLine reads /proc/<pid>/stat. The name sits in parentheses and
// may itself contain spaces or parentheses, so the fields after it start at
// the last ')'. It returns the starttime in clock ticks since boot.
func parseProcStatLine(raw []byte) (processInfo, int64, bool) {
	open := bytes.IndexByte(raw, '(')
	end := bytes.LastIndexByte(raw, ')')
	if open < 0 || end <= open {
		return processInfo{}, 0, false
	}
	fields := strings.Fields(string(raw[end+1:]))
	// state ppid pgrp session tty_nr tpgid ... starttime is proc field 22,
	// index 19 here.
	if len(fields) < 20 {
		return processInfo{}, 0, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	pgid, err2 := strconv.Atoi(fields[2])
	tpgid, err3 := strconv.Atoi(fields[5])
	start, err4 := strconv.ParseInt(fields[19], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return processInfo{}, 0, false
	}
	return processInfo{
		ppid:  ppid,
		pgid:  pgid,
		tpgid: tpgid,
		comm:  string(raw[open+1 : end]),
	}, start, true
}

func readCmdline(pid, comm string) string {
	raw, err := workerproc.ReadProcFile("/proc/" + pid + "/cmdline")
	if err != nil {
		return ""
	}
	raw = bytes.TrimRight(raw, "\x00")
	if len(raw) == 0 {
		return "[" + comm + "]"
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(string(raw), "\x00", " ")), " ")
}

func readBootTime() (int64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("proc process snapshot: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			boot, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err == nil && boot > 0 {
				return boot, nil
			}
		}
	}
	return 0, fmt.Errorf("proc process snapshot: no btime in /proc/stat")
}
