//go:build darwin && !screenfake

package screen

// The macOS backend is not in this build yet.
func newBackend() Backend {
	return unsupportedBackend{reason: ErrUnsupported.Error()}
}
