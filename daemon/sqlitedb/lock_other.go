//go:build !linux && !darwin

package sqlitedb

// lockFile is a no-op where flock is unavailable; those platforms do not
// run a second Mewla process against one state directory.
func lockFile(string) (func(), error) { return func() {}, nil }
