package workerproc

import (
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
				if env, err := os.ReadFile(root + "/environ"); err == nil {
					p.ResourceID, p.WorkerID = workerEnvIdentity(strings.Split(string(env), "\x00"))
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
func ParseProcStat(raw string) (Process, error) {
	end := strings.LastIndex(raw, ")")
	start := strings.Index(raw, "(")
	if start < 1 || end <= start {
		return Process{}, fmt.Errorf("invalid proc stat")
	}
	f := strings.Fields(raw[end+1:])
	if len(f) < 22 {
		return Process{}, fmt.Errorf("short proc stat")
	}
	if f[0] == "Z" || f[0] == "X" {
		return Process{}, fmt.Errorf("exited process")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(raw[:start]))
	if err != nil {
		return Process{}, err
	}
	p := Process{PID: pid, Args: raw[start+1 : end], Start: f[19]}
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
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", p.PID))
	if err != nil {
		return false
	}
	now, err := ParseProcStat(string(raw))
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
