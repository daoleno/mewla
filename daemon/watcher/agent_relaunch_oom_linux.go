package watcher

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// readOOMKills reads the kernel's oom_kill counter from /proc/vmstat.
func readOOMKills() (uint64, bool) {
	file, err := os.Open("/proc/vmstat")
	if err != nil {
		return 0, false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, ok := strings.Cut(scanner.Text(), " ")
		if !ok || name != "oom_kill" {
			continue
		}
		count, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		return count, err == nil
	}
	return 0, false
}
