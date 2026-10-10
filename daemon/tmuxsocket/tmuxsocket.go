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
