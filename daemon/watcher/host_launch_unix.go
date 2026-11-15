//go:build unix

package watcher

// hostLaunchArgv runs a Session's launch script under /bin/sh -c.
func hostLaunchArgv(script string) []string { return []string{"/bin/sh", "-c", script} }
