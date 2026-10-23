//go:build !linux && !darwin

package brain

// lockMigration is a no-op where flock is unavailable; those platforms do not
// run a second Mewla process against one state directory.
func lockMigration(string) (func(), error) { return func() {}, nil }
