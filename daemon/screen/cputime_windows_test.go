//go:build windows

package screen

import "time"

// cpuTime is not measured on Windows; the link harness reports zero.
func cpuTime() time.Duration { return 0 }
