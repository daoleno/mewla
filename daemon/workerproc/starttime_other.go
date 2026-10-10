//go:build !linux && !darwin

package workerproc

import "time"

// StartTime is unavailable on this platform: no /proc
// starttime evidence exists, so the second-granularity ps lstart value
// remains the process start evidence and the same-second instance-ownership
// limitation is a documented platform data limit (like zero startedAt).
func StartTime(pid int) (time.Time, bool) {
	return time.Time{}, false
}
