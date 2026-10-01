//go:build linux || darwin

package workerproc

import (
	"bufio"
	"strconv"
	"strings"
)

func largeLineScanner(out []byte) *bufio.Scanner {
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return scanner
}

func parseProcessIntegers(fields []string) ([]int64, bool) {
	values := make([]int64, len(fields))
	for index, field := range fields {
		parsed, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, false
		}
		values[index] = parsed
	}
	return values, true
}

func parseDarwinProcessSnapshot(out []byte, sessionID func(int) (int, error)) (map[int]processRecord, error) {
	processes := make(map[int]processRecord)
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		values, valid := parseProcessIntegers(fields[:4])
		if !valid || values[0] <= 0 {
			continue
		}
		pid := int(values[0])
		sid, _ := sessionID(pid)
		processes[pid] = processRecord{
			PID:  pid,
			PPID: int(values[1]),
			PGID: int(values[2]),
			SID:  sid,
			RSS:  uint64(max(values[3], 0)) * 1024,
			Args: strings.Join(fields[4:], " "),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return processes, nil
}
