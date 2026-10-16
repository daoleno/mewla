//go:build darwin

package computers

import (
	"os/exec"
	"strings"
)

// platformName is the name set in System Settings → General → About.
func platformName() string {
	out, err := exec.Command("scutil", "--get", "ComputerName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
