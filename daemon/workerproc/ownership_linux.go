package workerproc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func readProcesses(previous map[int]Process) (map[int]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	records := map[int]Process{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		root := filepath.Join("/proc", entry.Name())
		raw, err := os.ReadFile(root + "/stat")
		if err != nil {
			continue
		}
		p, err := ParseProcStat(string(raw))
		if err != nil {
			continue
		}
		if executable, err := os.Readlink(root + "/exe"); err == nil {
			p.Args = filepath.Base(strings.TrimSuffix(executable, " (deleted)"))
		}
		if info, err := os.Stat(root); err == nil {
			p.UID = int(info.Sys().(*syscall.Stat_t).Uid)
		}
		if old, ok := previous[pid]; ok && old.Start == p.Start {
			p.ResourceID = old.ResourceID
			p.WorkerID = old.WorkerID
			p.ContainerID = old.ContainerID
		} else {
			if p.UID == os.Getuid() {
				if env := Environ(pid); env != nil {
					p.ResourceID, p.WorkerID = workerEnvIdentity(env)
				}
			}
			if cg, err := os.ReadFile(root + "/cgroup"); err == nil {
				p.ContainerID = containerID(string(cg))
			}
		}
		records[pid] = p
	}
	return records, nil
}

// ErrExited reports a zombie or dead process: it holds no live identity.
var ErrExited = errors.New("exited process")

// procStatFields splits /proc/<pid>/stat into the pid, the comm and the
// fields after it, state first. The comm may contain spaces or parentheses,
// so the split is at the final ')'.
func procStatFields(raw string) (pid int, comm string, fields []string, err error) {
	end := strings.LastIndex(raw, ")")
	start := strings.Index(raw, "(")
	if start < 1 || end <= start {
		return 0, "", nil, fmt.Errorf("invalid proc stat")
	}
	pid, err = strconv.Atoi(strings.TrimSpace(raw[:start]))
	if err != nil {
		return 0, "", nil, err
	}
	return pid, raw[start+1 : end], strings.Fields(raw[end+1:]), nil
}

// ReadProcStat parses /proc/<pid>/stat. A missing process returns an
// os.IsNotExist error and a zombie returns ErrExited.
func ReadProcStat(pid int) (Process, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Process{}, err
	}
	return ParseProcStat(string(raw))
}

func ParseProcStat(raw string) (Process, error) {
	pid, comm, f, err := procStatFields(raw)
	if err != nil {
		return Process{}, err
	}
	if len(f) < 22 {
		return Process{}, fmt.Errorf("short proc stat")
	}
	if f[0] == "Z" || f[0] == "X" {
		return Process{}, ErrExited
	}
	p := Process{PID: pid, Args: comm, Start: f[19]}
	p.PPID, _ = strconv.Atoi(f[1])
	p.PGID, _ = strconv.Atoi(f[2])
	p.SID, _ = strconv.Atoi(f[3])
	utime, _ := strconv.ParseUint(f[11], 10, 64)
	stime, _ := strconv.ParseUint(f[12], 10, 64)
	p.Ticks = utime + stime
	rss, _ := strconv.ParseUint(f[21], 10, 64)
	p.RSS = rss * uint64(os.Getpagesize())
	return p, nil
}
func processIdentityMatches(p Process) bool {
	now, err := ReadProcStat(p.PID)
	return err == nil && now.Start == p.Start
}
func containerID(cgroup string) string {
	for _, part := range strings.FieldsFunc(cgroup, func(r rune) bool { return r == '/' || r == '\n' }) {
		part = strings.TrimSuffix(strings.TrimPrefix(part, "docker-"), ".scope")
		if len(part) == 64 {
			if _, err := strconv.ParseUint(part[:12], 16, 64); err == nil {
				return part[:12]
			}
		}
	}
	return ""
}
