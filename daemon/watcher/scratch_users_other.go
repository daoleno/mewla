//go:build darwin

package watcher

// scratchUsage is a process whose executable or working directory is inside a
// scratch directory.
type scratchUsage struct {
	PID int
	Exe string
}

// scratchUser: macOS has no /proc to read another process's cwd cheaply; no
// process is reported (the scratch of a gone Session is moved).
func scratchUser(string) (scratchUsage, bool) { return scratchUsage{}, false }
