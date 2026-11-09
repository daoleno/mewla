package sessionhost

import (
	"path"
	"strings"
)

// commandName shortens a process's argv[0] the way tmux names a window after
// it (parse_window_name): no leading "exec ", nothing after the first space,
// the base name, and no leading "-" of a login shell.
func commandName(argv0 string) string {
	name := strings.TrimSpace(argv0)
	name = strings.TrimSpace(strings.TrimPrefix(name, "exec "))
	if cut := strings.IndexByte(name, ' '); cut >= 0 {
		name = name[:cut]
	}
	name = strings.TrimRight(name, "-")
	if name == "" {
		return ""
	}
	name = path.Base(name)
	return strings.TrimLeft(name, "-")
}
