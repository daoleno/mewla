// Package statedir owns the default Mewla state root (~/.mewla).
package statedir

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// DirName is the state root under the user's home directory.
const DirName = ".mewla"

// RootEnv relocates the whole state root (identity, paired devices, Brain,
// Calendar, Work, worktrees, executors.toml) for a daemon and its CLI, without
// changing HOME: agent CLIs keep their sign-ins under the real HOME, so a
// throwaway daemon for onboarding or release tests cannot use a fresh HOME.
const RootEnv = "MEWLA_STATE_ROOT"

var override atomic.Pointer[string]

// Default returns the state root for home, or the relocated root.
func Default(home string) string {
	if root := override.Load(); root != nil {
		return *root
	}
	return filepath.Join(home, DirName)
}

// SetRoot relocates the state root for this process; "" restores the default.
// The path is resolved through any symlink first (macOS /tmp is one): private
// directories under the root refuse a symlinked path.
func SetRoot(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		override.Store(nil)
		return
	}
	dir = Canonical(dir)
	override.Store(&dir)
}

// Canonical resolves symlinks in path. Parts that do not exist yet are kept as
// written, under the resolved part that does; any error returns path unchanged.
func Canonical(path string) string {
	rest := ""
	for at := filepath.Clean(path); ; at = filepath.Dir(at) {
		if resolved, err := filepath.EvalSymlinks(at); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(at)
		if parent == at {
			return path
		}
		rest = filepath.Join(filepath.Base(at), rest)
	}
}

// InitFromEnv applies RootEnv. Only an absolute path counts: a relative one
// would move state with the working directory. It runs from main, so tests
// that pass their own home are never redirected by a Worker's environment.
func InitFromEnv() {
	dir := strings.TrimSpace(os.Getenv(RootEnv))
	if dir != "" && filepath.IsAbs(dir) {
		SetRoot(dir)
	}
}
