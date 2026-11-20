package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

// RefreshEnvironment brings this process's environment up to date with the
// person's, as a new terminal would see it. On Windows the daemon is started
// once at login, so a variable set afterwards (setx, System Properties, an
// installer adding itself to PATH) would otherwise never reach a Session or
// an agent lookup until the person logs in again; Unix shells read their rc
// files for each Session. Everywhere, an installer folder created since the
// daemon started (Claude Code's ~/.local/bin, which its installer leaves off
// PATH) joins PATH, so the agent in it is found without a restart. It does
// this at most once a second.
func RefreshEnvironment() {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if !lastRefresh.IsZero() && time.Since(lastRefresh) < time.Second {
		return
	}
	lastRefresh = time.Now()
	if fresh, err := userEnvironment(); err == nil && len(fresh) > 0 {
		for key, value := range environmentUpdates(os.Environ(), fresh) {
			_ = os.Setenv(key, value)
		}
	}
	hostdeps.AddInstallerDirsToPath()
}

var (
	refreshMu   sync.Mutex
	lastRefresh time.Time
	// pinned are the directories PinFirstOnPath keeps at the front of PATH.
	pinned []string
)

// PinFirstOnPath puts dir first on PATH and keeps it there through every
// refresh: the Mac app's daemon (its `mewla` in the app's Contents/Helpers)
// so that the Sessions, Workers and agents it starts run its own CLI, not an
// older copy a login shell's PATH names first.
func PinFirstOnPath(dir string) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if strings.TrimSpace(dir) == "" {
		return
	}
	pinned = append(pinned, dir)
	_ = os.Setenv("PATH", pinnedFirst(os.Getenv("PATH")))
}

func pinnedFirst(list string) string {
	if len(pinned) == 0 {
		return list
	}
	return mergePathList(strings.Join(pinned, string(os.PathListSeparator)), list)
}

// environmentUpdates is what to set so current matches fresh, the person's
// environment now: every variable fresh has with a different value, keyed as
// fresh spells it. PATH keeps fresh's order and then every directory current
// has that fresh lacks, so a directory the daemon added for itself (an
// installer's bin folder) stays. A variable only current has is kept: it came
// from how the daemon was started. Names compare as Windows compares them,
// without case; "=C:"-style entries are not variables.
func environmentUpdates(current, fresh []string) map[string]string {
	have := make(map[string]string, len(current))
	for _, entry := range current {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		have[strings.ToUpper(key)] = value
	}
	updates := map[string]string{}
	for _, entry := range fresh {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		folded := strings.ToUpper(key)
		old, known := have[folded]
		if folded == "PATH" && known {
			value = mergePathList(value, old)
		}
		if folded == "PATH" {
			value = pinnedFirst(value)
		}
		if known && old == value {
			continue
		}
		updates[key] = value
	}
	return updates
}

// mergePathList is fresh's directories in order, then current's that fresh
// does not have, compared without case or a trailing separator.
func mergePathList(fresh, current string) string {
	seen := map[string]bool{}
	var dirs []string
	add := func(list string) {
		for _, dir := range strings.Split(list, string(os.PathListSeparator)) {
			if strings.TrimSpace(dir) == "" {
				continue
			}
			key := strings.ToLower(strings.TrimRight(filepath.Clean(dir), `\/`))
			if seen[key] {
				continue
			}
			seen[key] = true
			dirs = append(dirs, dir)
		}
	}
	add(fresh)
	add(current)
	return strings.Join(dirs, string(os.PathListSeparator))
}

// KeepEnvironmentCurrent refreshes the environment every interval until ctx
// ends, so the daemon's own agent lookups (which agents are installed, which
// one Brain uses) see an agent installed after it started.
func KeepEnvironmentCurrent(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RefreshEnvironment()
		}
	}
}
