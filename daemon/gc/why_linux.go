//go:build linux

package gc

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/userunit"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// WhyProcess tells what holds the process pid, from /proc and the runtime's
// records: its Session slice, a lease that recorded it or an ancestor, the
// owned scratch it runs from, and last its environment.
func WhyProcess(l WhyLookup, pid int) (Why, error) {
	records, err := workerproc.Processes(true)
	if err != nil {
		return Why{}, err
	}
	record, ok := records[pid]
	if !ok {
		return Why{}, ErrNoProcess
	}
	base := filepath.Join("/proc", strconv.Itoa(pid))
	p := whyProc{pid: pid, envUnit: record.ResourceID}
	if raw, err := os.ReadFile(filepath.Join(base, "comm")); err == nil {
		p.name = strings.TrimSpace(string(raw))
	}
	if raw, err := os.ReadFile(filepath.Join(base, "cgroup")); err == nil {
		p.sessionTokens = userunit.SessionTokens(string(raw))
	}
	link := func(name string) string {
		target, _ := os.Readlink(filepath.Join(base, name))
		return strings.TrimSuffix(target, " (deleted)")
	}
	p.places = append(p.places, whyPlace{link("exe"), "its executable is there"}, whyPlace{link("cwd"), "its working directory is there"})
	if raw, err := os.ReadFile(filepath.Join(base, "cmdline")); err == nil {
		if dir := stateDirArg(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")); dir != "" {
			p.places = append(p.places, whyPlace{dir, "its --state-dir is there"})
		}
	}
	if raw, err := os.ReadFile(filepath.Join(base, "environ")); err == nil {
		for _, entry := range bytes.Split(raw, []byte{0}) {
			for _, name := range []string{"MEWLA_STATE_DIR", "MEWLA_STATE_ROOT"} {
				if value, ok := strings.CutPrefix(string(entry), name+"="); ok && value != "" {
					p.places = append(p.places, whyPlace{value, "its " + name + " is there"})
				}
			}
		}
	}
	return whyProcess(l, p, records), nil
}

// stateDirArg is the value of a --state-dir (or -state-dir) argument.
func stateDirArg(args []string) string {
	for i, arg := range args {
		for _, flag := range []string{"--state-dir", "-state-dir"} {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
			if value, ok := strings.CutPrefix(arg, flag+"="); ok {
				return value
			}
		}
	}
	return ""
}
