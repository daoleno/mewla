package workerproc

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"strconv"
	"strings"
)

func readProcesses(_ map[int]Process) (map[int]Process, error) {
	records, err := processSnapshot()
	if err != nil {
		return nil, err
	}
	// ps start timestamps fence reuse on macOS; unsupported CPU rates stay absent.
	raw, err := exec.Command("ps", "-axo", "pid=,lstart=").Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		p, ok := records[pid]
		if ok {
			p.Start = strings.Join(f[1:], " ")
			records[pid] = p
		}
	}

	for pid, p := range records {
		env := Environ(pid)
		if env == nil {
			continue
		}
		if resourceID, workerID := workerEnvIdentity(env); resourceID != "" || workerID != "" {
			p.ResourceID, p.WorkerID = resourceID, workerID
		}
		records[pid] = p
	}
	return records, nil
}
func processIdentityMatches(p Process) bool {
	raw, err := exec.Command("ps", "-p", strconv.Itoa(p.PID), "-o", "lstart=").Output()
	return err == nil && strings.Join(strings.Fields(string(raw)), " ") == p.Start && p.Start != ""
}

// kern.procargs2 separates argc/argv from the environment; ownership must
// never be inferred from a marker appearing in an executable argument.
func parseProcArgsEnvironment(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	if argc < 0 || argc > 65536 {
		return nil
	}
	rest := raw[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil
	}
	rest = rest[end+1:]
	rest = bytes.TrimLeft(rest, "\x00")
	for i := 0; i < argc; i++ {
		end = bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil
		}
		rest = rest[end+1:]
	}
	return strings.Split(string(rest), "\x00")
}
