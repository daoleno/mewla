//go:build windows

package work

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/daoleno/mewla/daemon/platform"
)

// Claude on Windows records procStart as the process's creation FILETIME.
// A record whose pid has no process, or a different one (a reused pid, as
// after a reboot), is not live.
func claudeProcessRecordLive(record claudeProcessSession) (bool, error) {
	started, err := platform.ProcessCreationFiletime(record.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("process start of %d: %w", record.PID, err)
	}
	if record.ProcStart == "" {
		return false, fmt.Errorf("Claude process %d has no start identity", record.PID)
	}
	want, err := strconv.ParseUint(record.ProcStart, 10, 64)
	if err != nil {
		return false, fmt.Errorf("Claude process %d start identity %q: %w", record.PID, record.ProcStart, err)
	}
	return started == want, nil
}
