//go:build windows

package watcher

// hostLaunchArgv runs a Session's launch script with `mewla sh -c`, which
// interprets the POSIX script in-process: Windows has no /bin/sh.
func hostLaunchArgv(script string) []string {
	return []string{ExecutablePath(), "sh", "-c", script}
}
