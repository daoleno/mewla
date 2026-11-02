//go:build !linux && !darwin

package connections

// Managed installers are unsupported here; providers return manual directions.
func lockCloudUpdate(string) (func(), error) { return func() {}, nil }
