package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// ClaudePromptCacheTTLEnv is Claude Code's switch for the prompt cache
// lifetime of the main conversation ("5m" or "1h"). Unset, Claude Code writes
// a 1-hour cache only on a subscription login within its usage limits and a
// 5-minute cache on an API key or a gateway, so a Session that waits more
// than five minutes (a build, a test, a reply, Brain woken by an event)
// rewrites its whole context at the next request.
const ClaudePromptCacheTTLEnv = "CLAUDE_CODE_PROMPT_CACHE_TTL"

const claudePromptCacheTTL = "1h"

// claudeForce5mEnv is Claude Code's switch that outranks every other TTL
// source and pins the 5-minute cache.
const claudeForce5mEnv = "FORCE_PROMPT_CACHING_5M"

// withClaudePromptCache returns env with the 1-hour prompt cache set for a
// Claude Code launch that runs on an API key or a gateway credential, and env
// unchanged for everything else: other agents, a subscription login (Claude
// Code already writes 1 hour there, within its limits, and an unset TTL lets
// it fall back to 5 minutes past them, which a forced 1 hour would not), and
// a launch whose person chose a TTL anywhere Claude Code reads one.
//
// The launch's credentials and TTL choice are what Claude Code itself would
// see: the Session env, the command's own env prefix, the daemon's env, and
// every settings source it reads for this launch: managed settings, the
// --settings payload (a routed Session's loopback token), the project's
// .claude/settings.local.json and .claude/settings.json under cwd, and the
// user-level settings.json of the effective config dir. The top-level
// "promptCacheTtl" key counts like the variable: a person's choice, from any of
// them. Settings files are only read.
func withClaudePromptCache(command, cwd string, env map[string]string) map[string]string {
	if !isClaudeCommand(command) {
		return env
	}
	launch := readClaudeLaunch(command, cwd, env)
	if launch.chosenTTL() || !launch.apiCredentials() {
		return env
	}
	out := cloneEnvironment(env)
	out[ClaudePromptCacheTTLEnv] = claudePromptCacheTTL
	return out
}

// claudeLaunch is the environment layers of one Claude Code launch, the
// highest priority first.
type claudeLaunch struct {
	layers    []map[string]string
	keyHelper bool
	// settingsTTL is a valid "promptCacheTtl" in any settings source.
	settingsTTL bool
}

// chosenTTL reports a TTL the person set anywhere Claude Code reads one.
func (l claudeLaunch) chosenTTL() bool {
	return l.settingsTTL || l.value(ClaudePromptCacheTTLEnv) != "" || l.value(claudeForce5mEnv) != ""
}

func (l *claudeLaunch) addSettings(settings claudeSettingsFile) {
	l.layers = append(l.layers, settings.Env)
	l.keyHelper = l.keyHelper || strings.TrimSpace(settings.APIKeyHelper) != ""
	l.settingsTTL = l.settingsTTL || settings.PromptCacheTTL != ""
}

func (l claudeLaunch) value(name string) string {
	for _, layer := range l.layers {
		if value := strings.TrimSpace(layer[name]); value != "" {
			return value
		}
	}
	return ""
}

// apiCredentials reports an Anthropic key or token (a gateway's included), or
// an apiKeyHelper, anywhere in the launch.
func (l claudeLaunch) apiCredentials() bool {
	return l.keyHelper || l.value("ANTHROPIC_API_KEY") != "" || l.value("ANTHROPIC_AUTH_TOKEN") != ""
}

func readClaudeLaunch(command, cwd string, env map[string]string) claudeLaunch {
	inline, settingsArg := parseClaudeCommand(command)
	process := map[string]string{}
	for _, name := range []string{ClaudePromptCacheTTLEnv, claudeForce5mEnv, "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CONFIG_DIR"} {
		process[name] = os.Getenv(name)
	}
	launch := claudeLaunch{layers: []map[string]string{inline, env, process}}
	// Claude Code's precedence, highest first: managed, --settings, project
	// local, shared project, user.
	for _, path := range claudeManagedSettingsFiles() {
		if settings, ok := readClaudeSettings(path); ok {
			launch.addSettings(settings)
		}
	}
	if settings, ok := readClaudeSettings(settingsArg); ok {
		launch.addSettings(settings)
	}
	for _, path := range claudeProjectSettingsFiles(cwd) {
		if settings, ok := readClaudeSettings(path); ok {
			launch.addSettings(settings)
		}
	}
	dir := launch.value("CLAUDE_CONFIG_DIR")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".claude")
		}
	}
	if dir != "" {
		if settings, ok := readClaudeSettings(filepath.Join(dir, "settings.json")); ok {
			launch.addSettings(settings)
		}
	}
	return launch
}

// claudeManagedSettingsDir is where Claude Code reads managed-settings.json
// and its managed-settings.d drop-in folder on this OS. Managed settings that
// arrive another way (server-managed, a macOS profile, the Windows registry)
// are not files and are not read here.
var claudeManagedSettingsDir = defaultClaudeManagedSettingsDir()

func defaultClaudeManagedSettingsDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	}
	return "/etc/claude-code"
}

func claudeManagedSettingsFiles() []string {
	dir := claudeManagedSettingsDir
	if dir == "" {
		return nil
	}
	files := []string{filepath.Join(dir, "managed-settings.json")}
	drops, _ := filepath.Glob(filepath.Join(dir, "managed-settings.d", "*.json"))
	sort.Strings(drops)
	return append(files, drops...)
}

// claudeProjectSettingsFiles lists the project settings of a Session started
// in cwd: both files under cwd, and the local file at the root of the git
// repository (the main checkout's root for a worktree), where Claude Code also
// reads it.
func claudeProjectSettingsFiles(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	files := []string{
		filepath.Join(cwd, ".claude", "settings.local.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
	}
	if root := claudeRepositoryRoot(cwd); root != "" && root != cwd {
		files = append(files, filepath.Join(root, ".claude", "settings.local.json"))
	}
	return files
}

// claudeRepositoryRoot is the root of the git repository holding dir; for a
// linked worktree, the main checkout's root. Empty outside a repository.
func claudeRepositoryRoot(dir string) string {
	for current := dir; ; {
		marker := filepath.Join(current, ".git")
		if info, err := os.Stat(marker); err == nil {
			if info.IsDir() {
				return current
			}
			data, err := os.ReadFile(marker)
			if err != nil {
				return current
			}
			// "gitdir: <main>/.git/worktrees/<name>"
			gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(current, gitdir)
			}
			if common := filepath.Dir(filepath.Dir(gitdir)); filepath.Base(filepath.Dir(gitdir)) == "worktrees" && filepath.Base(common) == ".git" {
				return filepath.Dir(common)
			}
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// parseClaudeCommand reads the leading "env NAME=value" prefix of a launch
// command and its last --settings argument (Claude Code keeps only the last).
func parseClaudeCommand(command string) (inline map[string]string, settings string) {
	inline = map[string]string{}
	fields, ok := shellquote.Fields(command)
	if !ok {
		fields = strings.Fields(command)
	}
	index := 0
	if len(fields) > 0 && fields[0] == "env" {
		for index = 1; index < len(fields) && looksLikeEnvAssignment(fields[index]); index++ {
			name, value, _ := strings.Cut(fields[index], "=")
			inline[name] = value
		}
	}
	for i := index; i < len(fields); i++ {
		switch {
		case fields[i] == "--settings" && i+1 < len(fields):
			settings = fields[i+1]
			i++
		case strings.HasPrefix(fields[i], "--settings="):
			settings = strings.TrimPrefix(fields[i], "--settings=")
		}
	}
	return inline, settings
}

type claudeSettingsFile struct {
	APIKeyHelper string
	Env          map[string]string
	// PromptCacheTTL is the top-level "promptCacheTtl" when it is one Claude
	// Code accepts ("5m" or "1h"); it ignores any other value.
	PromptCacheTTL string
}

// readClaudeSettings reads Claude Code settings from a JSON payload or a
// file path; ok is false when there are none or they do not parse.
func readClaudeSettings(source string) (claudeSettingsFile, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return claudeSettingsFile{}, false
	}
	raw := []byte(source)
	if !strings.HasPrefix(source, "{") {
		data, err := os.ReadFile(source)
		if err != nil {
			return claudeSettingsFile{}, false
		}
		raw = data
	}
	var parsed struct {
		APIKeyHelper   string         `json:"apiKeyHelper"`
		Env            map[string]any `json:"env"`
		PromptCacheTTL any            `json:"promptCacheTtl"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return claudeSettingsFile{}, false
	}
	// Claude Code accepts a number or a boolean as an env value; only the
	// strings matter here, and one of another type must not hide the rest.
	settings := claudeSettingsFile{APIKeyHelper: parsed.APIKeyHelper, Env: map[string]string{}}
	if ttl, ok := parsed.PromptCacheTTL.(string); ok && (ttl == "5m" || ttl == "1h") {
		settings.PromptCacheTTL = ttl
	}
	for name, value := range parsed.Env {
		if text, ok := value.(string); ok {
			settings.Env[name] = text
		}
	}
	return settings, true
}
