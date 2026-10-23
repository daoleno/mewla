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
)

// userHZ is the clock tick rate of /proc times. The kernel's user-space ABI
// fixes it at 100 on every Linux architecture, whatever CONFIG_HZ is.
const userHZ = 100

// cmdlineRefresh bounds how long a cached command line is trusted: exec keeps
// the pid and start time, so a process that re-execs under the same name
// changes its arguments without changing its identity.
const cmdlineRefresh = 10 * time.Second

type cachedCmdline struct {
	start  int64
	comm   string
	args   string
	readAt time.Time
}

var procSnapshots struct {
	sync.Mutex
	cmdlines map[int]cachedCmdline
	boot     int64
}

// snapshotProcessTable reads the process table from /proc in this process: no
// ps to fork and parse every few hundred milliseconds. Fields match the ps
// invocation it replaces: ids from stat, ucomm from the stat name, args from
// cmdline ("[name]" for a kernel thread, as ps shows it).
func snapshotProcessTable() (map[int]processInfo, error) {
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
	previous := procSnapshots.cmdlines
	next := make(map[int]cachedCmdline, len(previous))
	processes := make(map[int]processInfo, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue // exited since the directory listing
		}
		info, start, ok := parseProcStatLine(raw)
		if !ok {
			continue
		}
		info.pid = pid
		info.startedAt = time.Unix(procSnapshots.boot+start/userHZ, 0)

		cached, seen := previous[pid]
		if !seen || cached.start != start || cached.comm != info.comm || now.Sub(cached.readAt) >= cmdlineRefresh {
			cached = cachedCmdline{start: start, comm: info.comm, args: readCmdline(entry.Name(), info.comm), readAt: now}
		}
		next[pid] = cached
		info.args = cached.args
		processes[pid] = info
	}
	procSnapshots.cmdlines = next
	if len(processes) == 0 {
		return nil, fmt.Errorf("proc process snapshot found no processes")
	}
	return processes, nil
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
	raw, err := os.ReadFile("/proc/" + pid + "/cmdline")
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
