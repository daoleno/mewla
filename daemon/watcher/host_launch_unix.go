//go:build unix

package watcher

// hostLaunchArgv runs a Session's launch script as tmux runs a pane command.
func hostLaunchArgv(script string) []string { return []string{"/bin/sh", "-c", script} }
