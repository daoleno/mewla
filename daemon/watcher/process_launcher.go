package watcher

import "strings"

// launcherProcess is `mewla sh -c SCRIPT` (or the dev daemon's binary).
func launcherProcess(info processInfo) bool {
	if info.startedAt.IsZero() || !strings.HasPrefix(strings.ToLower(info.comm), "mewla") {
		return false
	}
	return strings.HasPrefix(commandLineArguments(info.args), "sh -c ")
}

// commandLineArguments is a Windows command line without its program, which
// is quoted when its path has spaces.
func commandLineArguments(line string) string {
	line = strings.TrimLeft(line, " \t")
	if strings.HasPrefix(line, `"`) {
		if end := strings.IndexByte(line[1:], '"'); end >= 0 {
			return strings.TrimLeft(line[end+2:], " \t")
		}
		return ""
	}
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return strings.TrimLeft(line[i:], " \t")
	}
	return ""
}
