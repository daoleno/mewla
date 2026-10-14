//go:build !darwin && !screenfake

package screen

import "os"

func newBackend() Backend { return unsupportedBackend{reason: ErrUnsupported.Error()} }

func computerName() string {
	name, _ := os.Hostname()
	return name
}
