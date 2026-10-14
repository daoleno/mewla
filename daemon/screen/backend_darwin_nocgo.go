//go:build darwin && !cgo && !screenfake

package screen

func newBackend() Backend {
	return unsupportedBackend{reason: "This Mewla build has no screen capture; install a release built for macOS"}
}
