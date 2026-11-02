//go:build !linux && !darwin && !windows

package connections

import (
	"errors"
	"os"
	"os/exec"
)

// Managed installation is unsupported here. Fail closed for retention rather
// than delete a generation without a working inter-process lease primitive.
func lockCloudFile(string, bool, bool) (*os.File, error) {
	return nil, errors.New("Cloud generation leases are unavailable on this platform")
}
func inheritCloudLease(*exec.Cmd, *os.File) {}
