// Package hostdeps names the one official command that installs a missing
// host dependency (tmux, an agent CLI) on the machine Mewla runs on. It only
// maps a detected platform to commands; callers decide whether to print or run
// them. Every command shown to a person is exactly the command that runs.
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

// TmuxInstallDoc is the official tmux install page, shown when no package
// manager is recognized.
const TmuxInstallDoc = "https://github.com/tmux/tmux/wiki/Installing"

// HomebrewInstall is the official Homebrew installer command from brew.sh.
const HomebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// homebrewBrew is where the Homebrew installer puts brew on Apple Silicon,
// the only Mac Mewla supports. A fresh install is not on PATH yet.
const homebrewBrew = "/opt/homebrew/bin/brew"

// Platform is what dependency commands depend on.
type Platform struct {
	GOOS     string // "linux" or "darwin"
	Distro   string // os-release ID, e.g. "ubuntu"
	IDLike   []string
	Pretty   string // os-release PRETTY_NAME or NAME
	WSL      bool
	Root     bool
	HasSudo  bool
	Brew     string // "brew" on PATH, its full path off PATH, "" when missing
	Manager  string // apt, dnf, yum, pacman, zypper, apk; "" when unknown
	Headless bool   // no local display or an SSH session
}

// Detect reads the running machine.
func Detect() Platform {
	osRelease, _ := os.ReadFile("/etc/os-release")
	procVersion, _ := os.ReadFile("/proc/version")
	headless := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" ||
		(runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "")
	return DetectFrom(runtime.GOOS, string(osRelease), string(procVersion), os.Geteuid(), headless, exec.LookPath)
}

// DetectFrom maps raw facts to a Platform; tests pass fixtures.
func DetectFrom(goos, osRelease, procVersion string, uid int, headless bool, lookPath func(string) (string, error)) Platform {
	p := Platform{GOOS: goos, Root: uid == 0, Headless: headless}
	has := func(name string) string {
		path, err := lookPath(name)
		if err != nil {
			return ""
		}
		return path
	}
	p.HasSudo = has("sudo") != ""
	if goos == "darwin" {
		p.Pretty = "macOS"
		if has("brew") != "" {
			p.Brew = "brew"
		} else if has(homebrewBrew) != "" {
			p.Brew = homebrewBrew
		}
		return p
	}
	fields := parseOSRelease(osRelease)
	p.Distro = strings.ToLower(fields["ID"])
	p.IDLike = strings.Fields(strings.ToLower(fields["ID_LIKE"]))
	p.Pretty = fields["PRETTY_NAME"]
	if p.Pretty == "" {
		p.Pretty = fields["NAME"]
	}
	p.WSL = strings.Contains(strings.ToLower(procVersion), "microsoft")
	p.Manager = packageManager(p, has)
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

func packageManager(p Platform, has func(string) string) string {
	family := append([]string{p.Distro}, p.IDLike...)
	in := func(names ...string) bool {
		return slices.ContainsFunc(family, func(id string) bool { return slices.Contains(names, id) })
	}
	switch {
	case in("debian", "ubuntu"):
		return "apt"
	case in("fedora", "rhel", "centos"):
		if has("dnf") != "" {
			return "dnf"
		}
		return "yum"
	case in("arch"):
		return "pacman"
	case in("opensuse", "suse", "opensuse-leap", "opensuse-tumbleweed", "sles"):
		return "zypper"
	case in("alpine"):
		return "apk"
	}
	// An unrecognized distribution: trust the one manager it has.
	for _, manager := range []string{"apt-get", "dnf", "pacman", "zypper", "apk"} {
		if has(manager) != "" {
			if manager == "apt-get" {
				return "apt"
			}
			return manager
		}
	}
	return ""
}

// Plan is the command sequence that installs one dependency. Doc replaces
// Steps when no command is known.
type Plan struct {
	Steps []string
	Doc   string
	// Blocked says why the steps cannot run here (no sudo); they still print.
	Blocked string
}

// Command is the whole plan as one line a person can paste.
func (plan Plan) Command() string {
	return strings.Join(plan.Steps, " && ")
}

// TmuxPlan returns the official tmux install for p.
func TmuxPlan(p Platform) Plan {
	if p.GOOS == "darwin" {
		if p.Brew != "" {
			return Plan{Steps: []string{p.Brew + " install tmux"}}
		}
		return Plan{Steps: []string{HomebrewInstall, homebrewBrew + " install tmux"}}
	}
	var commands []string
	switch p.Manager {
	case "apt":
		commands = []string{"apt update", "apt install -y tmux"}
	case "dnf":
		commands = []string{"dnf install -y tmux"}
	case "yum":
		commands = []string{"yum install -y tmux"}
	case "pacman":
		commands = []string{"pacman -S --needed --noconfirm tmux"}
	case "zypper":
		commands = []string{"zypper install -y tmux"}
	case "apk":
		commands = []string{"apk add tmux"}
	default:
		return Plan{Doc: TmuxInstallDoc}
	}
	plan := Plan{}
	for _, command := range commands {
		if !p.Root {
			command = "sudo " + command
		}
		plan.Steps = append(plan.Steps, command)
	}
	if !p.Root && !p.HasSudo {
		plan.Blocked = "sudo is not installed; run this as root without sudo"
	}
	return plan
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

// ExtraPathDirs are directories installers put binaries in that a fresh shell
// may not have on PATH yet: the native agent installers' ~/.local/bin and
// Homebrew on Apple Silicon.
func ExtraPathDirs(home string) []string {
	dirs := []string{}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin")
	}
	return dirs
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
// the tmux server it starts and a boot unit that copies PATH all find tools
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
