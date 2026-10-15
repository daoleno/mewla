// Package tmuxsocket binds tmux client invocations to an exact server.
package tmuxsocket

import "strings"

// Args returns the global flags binding an invocation to an externally owned
// server: -S selects the exact socket, and -N forbids the client from starting
// a server when that socket is absent or dead, so a missing server fails
// instead of forking a fallback into our own cgroup. An empty socketPath means
// the user's default server and adds no flags.
func Args(socketPath string) []string {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil
	}
	return []string{"-S", socketPath, "-N"}
}

// ClientArgs returns the global flags for every Mewla tmux client: -u plus
// Args. tmux treats a client as UTF-8 only when TMUX is set or LC_ALL,
// LC_CTYPE or LANG names UTF-8; otherwise it rewrites tabs, newlines and
// non-ASCII in command output (display-message -p, list-* -F) and in the
// attached client to "_". A launchd/LaunchAgent daemon has no LANG, so without
// -u every tab-separated format silently collapses into one field.
func ClientArgs(socketPath string) []string {
	return append([]string{"-u"}, Args(socketPath)...)
}
