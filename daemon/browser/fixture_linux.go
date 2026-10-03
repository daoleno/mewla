//go:build linux && browserfixture

package browser

// NewFixtureBackend is deliberately absent from ordinary product builds.
// Only disposable local test sites may use this unencrypted credential store.
func NewFixtureBackend() Backend { return &LinuxBackend{fixtureBasicStore: true} }
