package work

import (
	"os"
	"strings"
	"testing"
)

func TestMissingAgentFor(t *testing.T) {
	defer SetAgentLookPath(func(name string) (string, error) {
		if name == "codex" || name == "/opt/agents/claude" {
			return "/found/" + name, nil
		}
		return "", os.ErrNotExist
	})()
	for _, tc := range []struct {
		command string
		want    string // the program reported missing; "" when the launch is not refused
	}{
		{"", ""},
		{"bash", ""},
		{"my-script --flag", ""},
		{"cc foo.c", ""}, // not Claude Code's alias on a custom line
		{"codex --dangerously-bypass-approvals-and-sandbox", ""},
		{"/opt/agents/claude --x", ""},
		{"claude --permission-mode bypassPermissions", "claude"},
		{"env CLAUDE_CONFIG_DIR=/tmp/x claude", "claude"},
		{"CODEX_HOME=/tmp/x agy --dangerously-skip-permissions", "agy"},
		{"cursor-agent --force", "cursor-agent"},
		{"grok --no-alt-screen", "grok"},
		{"pi", "pi"},
		{"opencode --auto", "opencode"},
		{"dsh", "dsh"},
		{"'/usr/local/bin/claude' --x", "claude"},
	} {
		missing := MissingAgentFor(tc.command)
		got := ""
		if missing != nil {
			got = missing.Program
		}
		if got != tc.want {
			t.Errorf("MissingAgentFor(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
	if message := MissingAgentFor("pi").Message(); message != "Pi (pi) is not installed on this computer." {
		t.Fatalf("an agent Mewla has no installer for: %q", message)
	}
	if message := MissingAgentFor("agy").Message(); !strings.Contains(message, "Install it with: ") {
		t.Fatalf("agy: %q", message)
	}
}
