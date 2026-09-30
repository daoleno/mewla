//go:build linux

package work

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func claudeProcessRecordLive(record claudeProcessSession) (bool, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(record.PID) + "/stat")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return false, fmt.Errorf("invalid process stat for %d", record.PID)
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= 19 {
		return false, fmt.Errorf("incomplete process stat for %d", record.PID)
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return false, nil
	}
	if record.ProcStart == "" {
		return false, fmt.Errorf("Claude process %d has no start identity", record.PID)
	}
	return fields[19] == record.ProcStart, nil
}
