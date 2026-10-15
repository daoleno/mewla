package work

import (
	"strings"
	"testing"
)

func TestAgentToolsAreInvocationScopedAndCarryNoKey(t *testing.T) {
	both := AgentTools{Chrome: true, Computer: true}
	for _, command := range []string{"codex --model selected", "claude --model selected --dangerously-skip-permissions"} {
		got := WithAgentTools(command, "/path with space/mewla", "/private/state", both)
		if !strings.HasPrefix(got, command+" ") || !strings.Contains(got, "chrome-mcp") || !strings.Contains(got, "computer-mcp") || !strings.Contains(got, "/private/state") {
			t.Fatal(got)
		}
		if strings.Contains(got, "TOKEN") || strings.Contains(got, "--strict-mcp-config") {
			t.Fatal(got)
		}
		if _, ok := splitSupportedLaunchFields(got); !ok {
			t.Fatalf("unsafe command: %s", got)
		}
		// Launching again (a resume) does not add them twice.
		if again := WithAgentTools(got, "/path with space/mewla", "/private/state", both); again != got {
			t.Fatalf("added twice: %s", again)
		}
	}
	// Claude gets both servers in one config, so neither hides the other.
	claude := WithAgentTools("claude", "/mewla", "/state", both)
	if strings.Count(claude, "--mcp-config") != 1 || !strings.Contains(claude, "--allowedTools mcp__mewla_chrome,mcp__mewla_computer") {
		t.Fatal(claude)
	}
	// Codex passes the Worker identity to the computer server only.
	codex := WithAgentTools("codex", "/mewla", "/state", both)
	if strings.Count(codex, "env_vars=") != 1 || !strings.Contains(codex, `env_vars=["MEWLA_WORKER_ID"]`) {
		t.Fatal(codex)
	}
	if got := WithAgentTools("codex", "/mewla", "/state", AgentTools{Computer: true}); strings.Contains(got, "chrome") || !strings.Contains(got, "mewla_computer") {
		t.Fatal(got)
	}
}

func TestAgentToolsNeverBreakALaunch(t *testing.T) {
	both := AgentTools{Chrome: true, Computer: true}
	id := "d20f34e6-e54e-4ca2-baf8-bd49592ba60a"
	withBrowser, err := WithBrowserMCP("claude --model x", "/mewla", "/state", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"sh", "codex; echo bad", "codex --", "opencode", withBrowser, "claude --strict-mcp-config --mcp-config x"} {
		if got := WithAgentTools(command, "/mewla", "/state", both); got != command {
			t.Fatalf("changed %q into %q", command, got)
		}
	}
	if got := WithAgentTools("codex", "/mewla", "/state", AgentTools{}); got != "codex" {
		t.Fatal(got)
	}
	codexBrowser, err := WithBrowserMCP("codex", "/mewla", "/state", id)
	if err != nil {
		t.Fatal(err)
	}
	if got := WithAgentTools(codexBrowser, "/mewla", "/state", both); !strings.Contains(got, "mewla_browser") || !strings.Contains(got, ChromeMCPName) || !strings.Contains(got, ComputerMCPName) {
		t.Fatal(got)
	}
}
