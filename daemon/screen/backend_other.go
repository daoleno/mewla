//go:build !darwin && !linux && !windows && !screenfake

package screen

import (
	"os"

	"github.com/daoleno/mewla/daemon/vdisplay"
)

func newBackend() Backend { return unsupportedBackend{reason: ErrUnsupported.Error()} }

func computerName() string {
	name, _ := os.Hostname()
	return name
}

// ProcessPermissions has nothing to report without a permission model.
func ProcessPermissions() map[string]bool { return nil }

func platformBackend(*vdisplay.Service) Backend { return newBackend() }
