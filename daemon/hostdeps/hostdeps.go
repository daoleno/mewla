// Package hostdeps names the one official command that installs a missing
// host dependency (an agent CLI) on the machine Mewla runs on. It only maps a
// detected platform to commands; callers decide whether to print or run them.
// Every command shown to a person is exactly the command that runs.
package hostdeps

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Platform is what dependency commands depend on.
type Platform struct {
	GOOS     string // "linux" or "darwin"
	Pretty   string // os-release PRETTY_NAME or NAME
	WSL      bool
	Headless bool // no local display or an SSH session
}

// Detect reads the running machine.
func Detect() Platform {
	osRelease, _ := os.ReadFile("/etc/os-release")
	procVersion, _ := os.ReadFile("/proc/version")
	headless := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" ||
		(runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "")
	return DetectFrom(runtime.GOOS, string(osRelease), string(procVersion), headless)
}

// DetectFrom maps raw facts to a Platform; tests pass fixtures.
func DetectFrom(goos, osRelease, procVersion string, headless bool) Platform {
	p := Platform{GOOS: goos, Headless: headless}
	if goos == "darwin" {
		p.Pretty = "macOS"
		return p
	}
	fields := parseOSRelease(osRelease)
	p.Pretty = fields["PRETTY_NAME"]
	if p.Pretty == "" {
		p.Pretty = fields["NAME"]
	}
	p.WSL = strings.Contains(strings.ToLower(procVersion), "microsoft")
	return p
}

// Name is a short human name for messages, e.g. "Ubuntu 24.04 LTS (WSL)".
func (p Platform) Name() string {
	name := p.Pretty
	if name == "" {
		name = p.GOOS
	}
	if p.WSL {
		name += " (WSL)"
	}
	return name
}

func parseOSRelease(text string) map[string]string {
	fields := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		fields[key] = strings.Trim(value, `"'`)
	}
	return fields
}

// Plan is the command sequence that installs one dependency.
type Plan struct {
	Steps []string
}

// Command is the whole plan as one line a person can paste.
func (plan Plan) Command() string {
	return strings.Join(plan.Steps, " && ")
}

// OpenCodeInstall is OpenCode 2's official installer (opencode.ai/v2/docs).
// It also upgrades an OpenCode 1 install; doctor and first run name it when
// they find OpenCode 1, since Mewla supports OpenCode 2 only.
const OpenCodeInstall = "curl -fsSL https://opencode.ai/v2/install | bash"

// Agent is an agent CLI Mewla can offer to install.
type Agent struct {
	ID      string // executor id, e.g. "claude"
	Name    string
	Install Plan
	Login   string
}

// agentOrder lists executor ids in the one order Mewla uses everywhere: the
// install menu, doctor's recommended host and the Brain host picked when more
// than one agent CLI is ready. Claude Code comes first, as on mewla.ai.
var agentOrder = []string{"claude", "codex", "agent", "grok", "pi", "opencode"}

// agentCommands names the executable each agent id runs, where it differs from
// the id: Cursor's CLI is installed as cursor-agent.
var agentCommands = map[string]string{"agent": "cursor-agent"}

// AgentCommand is the executable the agent id launches.
func AgentCommand(id string) string {
	if command := agentCommands[id]; command != "" {
		return command
	}
	return id
}

// AgentOrder returns the executor ids in Mewla's preference order.
func AgentOrder() []string {
	return slices.Clone(agentOrder)
}

// AgentRank is id's place in AgentOrder; ids outside it sort after.
func AgentRank(id string) int {
	if index := slices.Index(agentOrder, id); index >= 0 {
		return index
	}
	return len(agentOrder)
}

// Agents returns the official install and sign-in commands for the agent CLIs
// first run offers, in AgentOrder. macOS, Linux and WSL share the official
// native installers (code.claude.com/docs/en/setup, github.com/openai/codex).
func Agents(p Platform) []Agent {
	if p.GOOS != "darwin" && p.GOOS != "linux" {
		return nil
	}
	codexLogin := "codex login"
	if p.Headless {
		codexLogin = "codex login --device-auth"
	}
	agents := []Agent{
		{
			ID:      "claude",
			Name:    "Claude Code",
			Install: Plan{Steps: []string{"curl -fsSL https://claude.ai/install.sh | bash"}},
			Login:   "claude auth login",
		},
		{
			ID:      "codex",
			Name:    "Codex",
			Install: Plan{Steps: []string{"curl -fsSL https://chatgpt.com/codex/install.sh | sh"}},
			Login:   codexLogin,
		},
	}
	slices.SortStableFunc(agents, func(a, b Agent) int { return AgentRank(a.ID) - AgentRank(b.ID) })
	return agents
}

// systemPathDirs are the installer directories outside the home directory:
// Homebrew on Apple Silicon.
var systemPathDirs = func() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/opt/homebrew/bin"}
	}
	return nil
}()

// SetSystemPathDirs replaces the installer directories outside the home
// directory and returns what puts them back. Tests use it to look only where
// they put fakes: a real Mac has Homebrew's agents in /opt/homebrew/bin.
func SetSystemPathDirs(dirs ...string) (restore func()) {
	previous := systemPathDirs
	systemPathDirs = slices.Clone(dirs)
	return func() { systemPathDirs = previous }
}

// ExtraPathDirs are directories installers put binaries in that a fresh shell
// may not have on PATH yet: the native agent installers' ~/.local/bin,
// OpenCode 2's ~/.opencode/bin and Homebrew on Apple Silicon.
func ExtraPathDirs(home string) []string {
	dirs := []string{}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".opencode", "bin"))
	}
	return append(dirs, systemPathDirs...)
}

// LookPath finds name on PATH, then in ExtraPathDirs, so a tool installed a
// moment ago is found before the shell is restarted.
func LookPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	} else if strings.Contains(name, "/") {
		return "", err
	}
	home, _ := os.UserHomeDir()
	for _, dir := range ExtraPathDirs(home) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

// AddToPath appends dir to this process's PATH when missing, so the daemon,
// the Sessions it starts and a boot unit that copies PATH all find tools
// installed there without changing which tool the user's PATH picks first.
func AddToPath(dir string) bool {
	if dir == "" {
		return false
	}
	current := os.Getenv("PATH")
	if slices.Contains(filepath.SplitList(current), dir) {
		return false
	}
	if current == "" {
		return os.Setenv("PATH", dir) == nil
	}
	return os.Setenv("PATH", current+string(os.PathListSeparator)+dir) == nil
}
