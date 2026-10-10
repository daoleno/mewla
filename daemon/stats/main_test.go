package stats

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Collector construction must never import a developer's live price cache.
	home, err := os.MkdirTemp("", "mewla-stats-test-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	// Provider paths resolve from the test's home, never a developer's
	// overrides or the installed opencode CLI's real database.
	for key, value := range map[string]string{
		"CLAUDE_CONFIG_DIR":   "",
		"PI_CODING_AGENT_DIR": "",
		"MEWLA_OPENCODE_DB":   home + "/no-opencode.db",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
