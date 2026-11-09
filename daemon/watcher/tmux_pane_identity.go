package watcher

import "strconv"

// isPaneID admits only tmux's immutable, server-local pane lifetime identifier.
// Names, windows and session-qualified targets are never execution identities.
func isPaneID(id string) bool {
	if len(id) < 2 || id[0] != '%' {
		return false
	}
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(id[1:], 10, 64)
	return err == nil
}
