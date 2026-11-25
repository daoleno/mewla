package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// withClaudePromptCache returns env with the 1-hour prompt cache set for a
// Claude Code launch that runs on an API key or a gateway credential, and env
// unchanged for everything else: other agents, a subscription login (Claude
// Code already writes 1 hour there, within its limits, and an unset TTL lets
// it fall back to 5 minutes past them, which a forced 1 hour would not), and
// a launch whose person chose a TTL anywhere Claude Code reads one.
//
// The launch's credentials are what Claude Code itself would see: the Session
// env, the command's own env prefix, the --settings payload (a routed
// Session's loopback token), the daemon's env and the user-level settings.json
// of the effective config dir. The user's settings.json is only read.
func withClaudePromptCache(command string, env map[string]string) map[string]string {
	if !isClaudeCommand(command) {
		return env
	}
	launch := readClaudeLaunch(command, env)
	if launch.value(ClaudePromptCacheTTLEnv) != "" || !launch.apiCredentials() {
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

func readClaudeLaunch(command string, env map[string]string) claudeLaunch {
	inline, settingsArg := parseClaudeCommand(command)
	process := map[string]string{}
	for _, name := range []string{ClaudePromptCacheTTLEnv, "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CONFIG_DIR"} {
		process[name] = os.Getenv(name)
	}
	launch := claudeLaunch{layers: []map[string]string{inline, env}}
	if settings, ok := readClaudeSettings(settingsArg); ok {
		launch.layers = append(launch.layers, settings.Env)
		launch.keyHelper = launch.keyHelper || strings.TrimSpace(settings.APIKeyHelper) != ""
	}
	launch.layers = append(launch.layers, process)
	dir := launch.value("CLAUDE_CONFIG_DIR")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".claude")
		}
	}
	if dir != "" {
		if settings, ok := readClaudeSettings(filepath.Join(dir, "settings.json")); ok {
			launch.layers = append(launch.layers, settings.Env)
			launch.keyHelper = launch.keyHelper || strings.TrimSpace(settings.APIKeyHelper) != ""
		}
	}
	return launch
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
		APIKeyHelper string         `json:"apiKeyHelper"`
		Env          map[string]any `json:"env"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return claudeSettingsFile{}, false
	}
	// Claude Code accepts a number or a boolean as an env value; only the
	// strings matter here, and one of another type must not hide the rest.
	settings := claudeSettingsFile{APIKeyHelper: parsed.APIKeyHelper, Env: map[string]string{}}
	for name, value := range parsed.Env {
		if text, ok := value.(string); ok {
			settings.Env[name] = text
		}
	}
	return settings, true
}
