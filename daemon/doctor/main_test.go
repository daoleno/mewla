package doctor

import (
	"os"
	"testing"
)

// TestMain keeps sign-in verdicts from reading this computer's own agent
// credentials: an API key in the environment or a provider in ~/.codex would
// turn every "signed out" fixture into "unknown".
func TestMain(m *testing.M) {
	for _, name := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		_ = os.Unsetenv(name)
	}
	empty, err := os.MkdirTemp("", "doctor-agent-config-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("CODEX_HOME", empty)
	_ = os.Setenv("CLAUDE_CONFIG_DIR", empty)
	code := m.Run()
	_ = os.RemoveAll(empty)
	os.Exit(code)
}
