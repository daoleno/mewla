//go:build darwin && !screenfake

package screen

import "github.com/daoleno/mewla/daemon/vdisplay"

// A Mac's agents use its own screen; the agent screen is Linux's.
func platformBackend(*vdisplay.Service) Backend { return newBackend() }
