package work

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/shellquote"
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
		if _, ok := shellquote.Fields(got); !ok {
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

func TestTheSwitchTurnsOffCodexsOwnComputerUse(t *testing.T) {
	off := AgentTools{NoCodexComputerUse: true}
	got := WithAgentTools("codex --model selected", "/mewla", "/state", off)
	if got != "codex --model selected -c mcp_servers.computer-use.enabled=false" {
		t.Fatal(got)
	}
	if _, ok := shellquote.Fields(got); !ok {
		t.Fatalf("unsafe command: %s", got)
	}
	if again := WithAgentTools(got, "/mewla", "/state", off); again != got {
		t.Fatalf("added twice: %s", again)
	}
	// Claude has no such tool; a launch with its own Codex home may not
	// define the server, and Codex refuses to start with half an entry.
	for _, command := range []string{"claude", "env CODEX_HOME=/elsewhere codex", "codex -c mcp_servers.computer-use.enabled=true"} {
		if got := WithAgentTools(command, "/mewla", "/state", off); got != command {
			t.Fatalf("changed %q into %q", command, got)
		}
	}
}

func TestThePhoneIsAnInvocationScopedServerForClaudeAndCodex(t *testing.T) {
	phone := AgentTools{Phone: true}
	codex := WithAgentTools("codex --model x", "/path with space/mewla", "/private/state", phone)
	for _, want := range []string{
		"mcp_servers.mewla_phone={", `args=["agent-use","phone-mcp","--state-dir","/private/state"]`, ",enabled=true", `env_vars=["MEWLA_WORKER_ID"]`,
		// A handoff waits ten minutes for the person; Codex's default minute would cut it short.
		",tool_timeout_sec=800",
	} {
		if !strings.Contains(codex, want) {
			t.Fatalf("missing %q in %s", want, codex)
		}
	}
	if strings.Contains(codex, "chrome") || strings.Contains(codex, "computer-mcp") {
		t.Fatal(codex)
	}
	claude := WithAgentTools("claude --model x", "/path with space/mewla", "/private/state", phone)
	if !strings.Contains(claude, `"timeout":800000`) {
		t.Fatalf("Claude's per-server tool limit is not set to cover a handoff: %s", claude)
	}
	if !strings.Contains(claude, `"mewla_phone"`) || !strings.Contains(claude, "phone-mcp") || !strings.HasSuffix(claude, "--allowedTools mcp__mewla_phone") {
		t.Fatal(claude)
	}
	for _, got := range []string{codex, claude} {
		if _, ok := shellquote.Fields(got); !ok {
			t.Fatalf("unsafe command: %s", got)
		}
		if again := WithAgentTools(got, "/path with space/mewla", "/private/state", phone); again != got {
			t.Fatalf("added twice: %s", again)
		}
	}
	// Alongside the others, in one Claude config; off means absent.
	all := WithAgentTools("claude", "/mewla", "/state", AgentTools{Chrome: true, Computer: true, Phone: true})
	if strings.Count(all, "--mcp-config") != 1 || !strings.Contains(all, "--allowedTools mcp__mewla_chrome,mcp__mewla_computer,mcp__mewla_phone") {
		t.Fatal(all)
	}
	if got := WithAgentTools("codex", "/mewla", "/state", AgentTools{Chrome: true, Computer: true}); strings.Contains(got, "phone") {
		t.Fatal(got)
	}
	// A launch that names MCP servers itself is left alone.
	if got := WithAgentTools("claude --mcp-config x", "/mewla", "/state", phone); got != "claude --mcp-config x" {
		t.Fatal(got)
	}
	if got := WithAgentTools("codex -c 'mcp_servers.mewla_phone={command=\"x\"}'", "/mewla", "/state", phone); strings.Count(got, "mewla_phone") != 1 {
		t.Fatal(got)
	}
}
