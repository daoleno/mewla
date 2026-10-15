//go:build !linux

package agentuse

// findDesktop: on macOS Mewla runs in the user's login session, which is
// where Chrome opens.
func findDesktop() ([]string, bool) { return nil, true }
