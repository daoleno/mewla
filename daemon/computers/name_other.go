//go:build !darwin

package computers

import "os"

// machineInfoPath holds the pretty hostname systemd keeps; reading it needs
// no D-Bus, so it works on a headless server too.
var machineInfoPath = "/etc/machine-info"

func platformName() string {
	contents, err := os.ReadFile(machineInfoPath)
	if err != nil {
		return ""
	}
	return prettyHostname(string(contents))
}
