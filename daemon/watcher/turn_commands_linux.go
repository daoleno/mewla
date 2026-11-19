//go:build linux

package watcher

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"time"
)

// preciseProcessStart is when pid started, to the clock tick, and false for a
// process that is gone or a zombie. It reads the boot clock from /proc/uptime:
// /proc/stat's btime is whole seconds, which would move every start by up to
// one.
func preciseProcessStart(pid int) (time.Time, bool) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	uptime, err := os.ReadFile("/proc/uptime")
	now := time.Now()
	if err != nil {
		return time.Time{}, false
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}, false
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	upFields := strings.Fields(string(uptime))
	if len(upFields) == 0 {
		return time.Time{}, false
	}
	up, err := strconv.ParseFloat(upFields[0], 64)
	if err != nil {
		return time.Time{}, false
	}
	sinceStart := time.Duration(up*float64(time.Second)) - time.Duration(ticks)*time.Second/userHZ
	return now.Add(-sinceStart), true
}
