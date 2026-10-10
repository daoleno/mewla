// Package providerpaths resolves where provider CLIs keep their local data,
// honouring the same overrides the CLIs do. Work (transcripts, live identity)
// and stats (usage totals) both read provider data through it, so they always
// look in the same place.
package providerpaths

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ClaudeConfigDir is Claude Code's config root: CLAUDE_CONFIG_DIR, else
// ~/.claude.
func ClaudeConfigDir(home string) string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	return filepath.Join(home, ".claude")
}

// PiAgentDir is Pi's agent root: PI_CODING_AGENT_DIR (a leading ~ expands to
// home), else ~/.pi/agent. Sessions live in its "sessions" directory.
func PiAgentDir(home string) string {
	override := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
	switch {
	case override == "":
		return filepath.Join(home, ".pi", "agent")
	case override == "~":
		return home
	case strings.HasPrefix(override, "~/"):
		return filepath.Join(home, strings.TrimPrefix(override, "~/"))
	default:
		return override
	}
}

// openCodeDBResolved memoizes a successful OpenCode SQLite path resolution.
// The `opencode db path` CLI spawn is expensive (the opencode process startup
// can take hundreds of milliseconds), so it must never run per poll. A failed
// resolution is NOT cached: discovery is retried on later calls so a
// transient failure (or a late-arriving opencode install) self-corrects. The
// MEWLA_OPENCODE_DB override is re-read every call (tests set it
// dynamically).
var (
	openCodeDBMu       sync.Mutex
	openCodeDBResolved string
)

// OpenCodeDB is OpenCode's SQLite database: MEWLA_OPENCODE_DB, else what
// `opencode db path` reports, else the platform data directory when the file
// exists there. It returns "" when no database is found.
func OpenCodeDB(home string) string {
	if override := strings.TrimSpace(os.Getenv("MEWLA_OPENCODE_DB")); override != "" {
		return override
	}
	openCodeDBMu.Lock()
	defer openCodeDBMu.Unlock()
	if openCodeDBResolved != "" {
		return openCodeDBResolved
	}
	if path := openCodeCLIDBPath(); path != "" {
		openCodeDBResolved = path
		return path
	}
	if home == "" {
		return ""
	}
	fallback := openCodeDefaultDBPath(home)
	if _, err := os.Stat(fallback); err == nil {
		openCodeDBResolved = fallback
		return fallback
	}
	return ""
}

func openCodeCLIDBPath() string {
	binary, err := exec.LookPath("opencode")
	if err != nil {
		return ""
	}
	out, err := exec.Command(binary, "db", "path").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// openCodeDefaultDBPath mirrors the official CLI's platform layout:
// $XDG_DATA_HOME on Linux, Application Support on macOS and LOCALAPPDATA on
// Windows.
func openCodeDefaultDBPath(home string) string {
	var dataDir string
	switch {
	case strings.TrimSpace(os.Getenv("XDG_DATA_HOME")) != "":
		dataDir = filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	case runtime.GOOS == "darwin":
		dataDir = filepath.Join(home, "Library", "Application Support", "opencode")
	case runtime.GOOS == "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			dataDir = filepath.Join(local, "opencode")
		} else {
			dataDir = filepath.Join(home, "AppData", "Local", "opencode")
		}
	default:
		dataDir = filepath.Join(home, ".local", "share", "opencode")
	}
	return filepath.Join(dataDir, "opencode.db")
}
