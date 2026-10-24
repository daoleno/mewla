package computers

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

var (
	capsMu     sync.Mutex
	capsCached Caps
	capsAt     time.Time
)

// DetectCaps reports what this computer offers another Brain: its agents and,
// on macOS, its installed apps (names only). Nothing is configured; it looks.
func DetectCaps() Caps {
	capsMu.Lock()
	defer capsMu.Unlock()
	if time.Since(capsAt) < 10*time.Minute {
		return capsCached
	}
	hostname, _ := os.Hostname()
	caps := Caps{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: ShortHostname(hostname)}
	home, _ := os.UserHomeDir()
	for _, id := range hostdeps.AgentOrder() {
		if _, err := hostdeps.LookPath(hostdeps.AgentCommand(id)); err == nil {
			caps.Agents = append(caps.Agents, id)
		}
	}
	if runtime.GOOS == "darwin" {
		dirs := []string{"/Applications", "/System/Applications", "/Applications/Utilities"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Applications"))
		}
		caps.Apps = listApps(dirs)
	}
	capsCached, capsAt = caps, time.Now()
	return caps
}

func listApps(dirs []string) []string {
	seen := map[string]bool{}
	var apps []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".app") || strings.HasPrefix(name, ".") {
				continue
			}
			name = strings.TrimSuffix(name, ".app")
			if !seen[name] {
				seen[name] = true
				apps = append(apps, name)
			}
		}
	}
	sort.Strings(apps)
	if len(apps) > 400 {
		apps = apps[:400]
	}
	return apps
}

// ShortHostname drops a local domain: "studio.local" becomes "studio".
func ShortHostname(hostname string) string {
	hostname = strings.TrimSpace(hostname)
	if index := strings.Index(hostname, "."); index > 0 {
		hostname = hostname[:index]
	}
	if hostname == "" {
		return "computer"
	}
	return hostname
}
