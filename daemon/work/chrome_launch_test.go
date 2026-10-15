package work

import (
	"strings"
	"testing"
)

func TestChromeLaunchAddsTheUsersChromeWithoutItsKey(t *testing.T) {
	for _, command := range []string{"codex --model selected", "claude --model selected --dangerously-skip-permissions"} {
		got := WithChromeMCP(command, "/path with space/mewla", "/private/state")
		if !strings.HasPrefix(got, command+" ") || !strings.Contains(got, ChromeMCPName) || !strings.Contains(got, "chrome-mcp") || !strings.Contains(got, "/private/state") {
			t.Fatal(got)
		}
		if strings.Contains(got, "TOKEN") || strings.Contains(got, "--strict-mcp-config") {
			t.Fatal(got)
		}
		if _, ok := splitSupportedLaunchFields(got); !ok {
			t.Fatalf("unsafe command: %s", got)
		}
		// Launching again (a resume) does not add it twice.
		if again := WithChromeMCP(got, "/path with space/mewla", "/private/state"); again != got && InferWorkerProvider(command) == WorkerProviderCodex {
			t.Fatalf("added twice: %s", again)
		}
	}
}

func TestChromeLaunchNeverBreaksALaunch(t *testing.T) {
	id := "d20f34e6-e54e-4ca2-baf8-bd49592ba60a"
	withBrowser, err := WithBrowserMCP("claude --model x", "/mewla", "/state", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"sh", "codex; echo bad", "codex --", "opencode", withBrowser, "claude --strict-mcp-config --mcp-config x"} {
		if got := WithChromeMCP(command, "/mewla", "/state"); got != command {
			t.Fatalf("changed %q into %q", command, got)
		}
	}
	// Codex keeps an attached Mewla Browser and gains the user's Chrome too.
	codexBrowser, err := WithBrowserMCP("codex", "/mewla", "/state", id)
	if err != nil {
		t.Fatal(err)
	}
	if got := WithChromeMCP(codexBrowser, "/mewla", "/state"); !strings.Contains(got, "mewla_browser") || !strings.Contains(got, ChromeMCPName) {
		t.Fatal(got)
	}
}
