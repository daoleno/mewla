package sessionhost

import "testing"

func TestCommandName(t *testing.T) {
	for argv0, want := range map[string]string{
		"claude":              "claude",
		"/usr/bin/zsh":        "zsh",
		"-zsh":                "zsh",
		"exec /bin/bash -l":   "bash",
		"node /x/cli.js":      "node",
		"  /opt/codex/codex ": "codex",
		"":                    "",
	} {
		if got := commandName(argv0); got != want {
			t.Errorf("commandName(%q) = %q, want %q", argv0, got, want)
		}
	}
}
