//go:build !linux

package work

import "fmt"

// Other platforms retain command-line recovery. Never trust a process registry
// until its provider procStart representation has a platform-specific verifier.
func claudeProcessRecordLive(record claudeProcessSession) (bool, error) {
	return false, fmt.Errorf("Claude process registry start identity verification unavailable on this platform (pid %d)", record.PID)
}
