//go:build !linux

package watcher

// readOOMKills has no kernel counter to read outside Linux.
func readOOMKills() (uint64, bool) { return 0, false }
