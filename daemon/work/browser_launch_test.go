package work

import (
	"strings"
	"testing"
)

func TestBrowserLaunchIsInvocationScopedAndPreservesProviderOptions(t *testing.T) {
	id := "d20f34e6-e54e-4ca2-baf8-bd49592ba60a"
	for _, command := range []string{"codex --model selected", "claude --model selected"} {
		got, err := WithBrowserMCP(command, "/path with space/zen", "/private/state", id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, command+" ") || !strings.Contains(got, "zen_browser") || !strings.Contains(got, id) || strings.Contains(got, "--strict-mcp-config") || strings.Contains(got, "--task") {
			t.Fatal(got)
		}
		if _, ok := splitSupportedLaunchFields(got); !ok {
			t.Fatalf("unsafe command: %s", got)
		}
	}
	for _, command := range []string{"sh", "codex; echo bad", "codex --", "codex -c mcp_servers.zen_browser={}"} {
		if _, err := WithBrowserMCP(command, "/zen", "/state", id); err == nil {
			t.Fatalf("accepted %s", command)
		}
	}
}
