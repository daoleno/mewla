package doctor

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/providerpaths"
	"github.com/daoleno/mewla/daemon/work"
)

// accountLabel is who an agent CLI is signed in as, read from the identity the
// CLI itself recorded: an email address, nothing secret. It returns "" when
// the CLI keeps none (an API key, a token in the environment).
func accountLabel(provider, configDir string) string {
	switch provider {
	case work.WorkerProviderClaude:
		return claudeAccount(configDir)
	case work.WorkerProviderCodex:
		return codexAccount(configDir)
	}
	return ""
}

// claudeAccount reads oauthAccount from Claude Code's global state file: a
// custom config directory keeps it inside, the default one beside ~/.claude.
func claudeAccount(configDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".claude.json")
	switch {
	case configDir != "":
		path = filepath.Join(configDir, ".claude.json")
	case os.Getenv("CLAUDE_CONFIG_DIR") != "":
		path = filepath.Join(providerpaths.ClaudeConfigDir(home), ".claude.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state struct {
		OAuthAccount struct {
			Email string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return ""
	}
	return cleanAccount(state.OAuthAccount.Email)
}

// codexAccount reads the email claim of the ID token Codex stored at sign-in.
func codexAccount(configDir string) string {
	codexHome := configDir
	if codexHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		codexHome = providerpaths.CodexHome(userHome)
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		return ""
	}
	var auth struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return ""
	}
	parts := strings.Split(auth.Tokens.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return cleanAccount(claims.Email)
}

func cleanAccount(email string) string {
	email = strings.TrimSpace(email)
	if len(email) > 120 || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n\x1b") {
		return ""
	}
	return email
}
