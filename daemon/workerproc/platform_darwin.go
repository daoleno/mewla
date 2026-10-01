//go:build darwin

package workerproc

import (
	"os/exec"
	"strings"
)

func bootID() string {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
