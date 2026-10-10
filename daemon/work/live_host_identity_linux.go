//go:build linux

package work

import (
	"errors"
	"fmt"
	"os"

	"github.com/daoleno/mewla/daemon/workerproc"
)

func claudeProcessRecordLive(record claudeProcessSession) (bool, error) {
	process, err := workerproc.ReadProcStat(record.PID)
	if os.IsNotExist(err) || errors.Is(err, workerproc.ErrExited) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("invalid process stat for %d: %w", record.PID, err)
	}
	if record.ProcStart == "" {
		return false, fmt.Errorf("Claude process %d has no start identity", record.PID)
	}
	return process.Start == record.ProcStart, nil
}
