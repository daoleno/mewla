package work

import (
	"strings"
	"testing"
)

func TestConfigDirBecomesAnEnvPrefixedCommand(t *testing.T) {
	claude := NewWorkerExecutor("claude-work", Executor{Name: "claude-work", Command: "claude --model m", ConfigDir: "/home/u/.claude_work"})
	if claude.Command != "env CLAUDE_CONFIG_DIR=/home/u/.claude_work claude --model m" || claude.BareCommand != "claude --model m" || claude.ConfigDir != "/home/u/.claude_work" {
		t.Fatalf("claude = %#v", claude)
	}
	codex := NewWorkerExecutor("codex-2", Executor{Name: "codex-2", Command: "codex", ConfigDir: "/home/u/.codex 2"})
	if !strings.HasPrefix(codex.Command, "env CODEX_HOME=") || !strings.HasSuffix(codex.Command, " codex") || strings.Contains(codex.Command, "CODEX_HOME=/home/u/.codex 2 ") {
		t.Fatalf("codex command must quote a directory with a space: %q", codex.Command)
	}
	for name, e := range map[string]Executor{
		"relative":    {Command: "claude", ConfigDir: "rel/dir"},
		"unsupported": {Command: "grok", ConfigDir: "/x"},
		"none":        {Command: "claude"},
		"already set": {Command: "env CLAUDE_CONFIG_DIR=/a claude", ConfigDir: "/b"},
	} {
		got := NewWorkerExecutor("x", e)
		if got.Command != e.Command || (got.ConfigDir != "" && name != "already set") {
			t.Errorf("%s: %#v", name, got)
		}
	}
}

func TestLaunchOfAnInstanceBelongsToThatInstance(t *testing.T) {
	cfg := NewExecutorConfig(map[string]Executor{
		"claude":      {Name: "claude", Command: "claude"},
		"claude-work": {Name: "claude-work", Command: "claude", ConfigDir: "/home/u/.claude_work"},
	})
	command, err := cfg.ResolveWorkerCommand("claude-work", "", "", "", true)
	if err != nil || !strings.HasPrefix(command, "env CLAUDE_CONFIG_DIR=/home/u/.claude_work claude") || !strings.Contains(command, "bypassPermissions") {
		t.Fatalf("command = %q, %v", command, err)
	}
	if got := cfg.IDForCommand(command); got != "claude-work" {
		t.Fatalf("IDForCommand(instance launch) = %q", got)
	}
	if got := cfg.IDForCommand("claude --model m"); got != "claude" {
		t.Fatalf("IDForCommand(plain) = %q; the default sign-in keeps the plain launch", got)
	}
}
