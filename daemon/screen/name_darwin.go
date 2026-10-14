//go:build darwin && !screenfake

package screen

import (
	"os"
	"os/exec"
	"strings"
)

// computerName is the name set in System Settings → General → About.
func computerName() string {
	if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	name, _ := os.Hostname()
	return name
}
