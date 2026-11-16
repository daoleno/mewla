package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/mewla/daemon/work"
)

// An agent's login status speaks only for its own sign-in. Codex runs on an
// OPENAI_API_KEY or a model provider of its own (config.toml model_provider,
// a gateway with its own key), and Claude Code on an Anthropic key or an
// apiKeyHelper, while their status commands say "not logged in". Such an
// agent is not known to be signed out: its verdict is AuthUnknown.

// otherCredentials reports whether the agent has a way to run that its login
// status does not see.
func otherCredentials(provider, configDir string) bool {
	switch provider {
	case work.WorkerProviderCodex:
		return envSet("OPENAI_API_KEY", "CODEX_API_KEY") || codexOwnProvider(codexHome(configDir))
	case work.WorkerProviderClaude:
		return envSet("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN") || claudeSettingsCredentials(claudeConfigDir(configDir))
	}
	return false
}

func envSet(names ...string) bool {
	for _, name := range names {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

func codexHome(configDir string) string {
	if dir := strings.TrimSpace(configDir); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// codexOwnProvider reports a config.toml that selects a model provider other
// than OpenAI's own sign-in.
func codexOwnProvider(dir string) bool {
	if dir == "" {
		return false
	}
	var config struct {
		ModelProvider string `toml:"model_provider"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "config.toml"), &config); err != nil {
		return false
	}
	provider := strings.TrimSpace(config.ModelProvider)
	return provider != "" && !strings.EqualFold(provider, "openai")
}

func claudeConfigDir(configDir string) string {
	if dir := strings.TrimSpace(configDir); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeSettingsCredentials reports a settings.json with an apiKeyHelper or an
// Anthropic key or token in its env.
func claudeSettingsCredentials(dir string) bool {
	if dir == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return false
	}
	var settings struct {
		APIKeyHelper string            `json:"apiKeyHelper"`
		Env          map[string]string `json:"env"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return false
	}
	if strings.TrimSpace(settings.APIKeyHelper) != "" {
		return true
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if strings.TrimSpace(settings.Env[name]) != "" {
			return true
		}
	}
	return false
}
