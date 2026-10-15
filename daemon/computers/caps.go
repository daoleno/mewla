package computers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// agentCommands are the agent CLIs a computer reports, by executable name.
var agentCommands = []struct{ name, command string }{
	{"claude", "claude"},
	{"codex", "codex"},
	{"agent", "cursor-agent"},
	{"grok", "grok"},
	{"pi", "pi"},
	{"opencode", "opencode"},
}

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
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home != "" {
		extra = append(extra, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".bun", "bin"), filepath.Join(home, ".npm-global", "bin"))
	}
	for _, agent := range agentCommands {
		if findCommand(agent.command, extra) {
			caps.Agents = append(caps.Agents, agent.name)
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

func findCommand(command string, extra []string) bool {
	if _, err := exec.LookPath(command); err == nil {
		return true
	}
	for _, dir := range extra {
		if info, err := os.Stat(filepath.Join(dir, command)); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
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
