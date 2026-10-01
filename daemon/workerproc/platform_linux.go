//go:build linux

package workerproc

import (
	"os"
	"strings"
)

func bootID() string {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
