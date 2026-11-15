package platform

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// RefreshEnvironment brings this process's environment up to date with the
// person's, as a new terminal would see it. On Windows the daemon is started
// once at login, so a variable set afterwards (setx, System Properties, an
// installer adding itself to PATH) would otherwise never reach a Session or
// an agent lookup until the person logs in again. Unix shells read their rc
// files for each Session, so there it does nothing. It reads the user's
// environment at most once a second.
func RefreshEnvironment() {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if !lastRefresh.IsZero() && time.Since(lastRefresh) < time.Second {
		return
	}
	lastRefresh = time.Now()
	fresh, err := userEnvironment()
	if err != nil || len(fresh) == 0 {
		return
	}
	for key, value := range environmentUpdates(os.Environ(), fresh) {
		_ = os.Setenv(key, value)
	}
}

var (
	refreshMu   sync.Mutex
	lastRefresh time.Time
)

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
// one Brain uses) see an agent installed after it started. It returns at once
// where RefreshEnvironment has nothing to do.
func KeepEnvironmentCurrent(ctx context.Context, interval time.Duration) {
	if runtime.GOOS != "windows" {
		return
	}
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
