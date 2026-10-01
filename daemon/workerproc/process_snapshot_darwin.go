//go:build darwin

package workerproc

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

func processSnapshot() (map[int]processRecord, error) {
	// Darwin's ps "sess" keyword is a kernel session pointer, not the numeric
	// POSIX session ID. Query getsid(2) for each visible PID instead.
	out, err := exec.Command("ps", "-ww", "-axo", "pid=,ppid=,pgid=,rss=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseDarwinProcessSnapshot(out, unix.Getsid)
}
