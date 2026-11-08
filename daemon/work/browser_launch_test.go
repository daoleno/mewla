package work

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/shellquote"
)

func TestBrowserLaunchIsInvocationScopedAndPreservesProviderOptions(t *testing.T) {
	for _, command := range []string{"codex --model selected", "claude --model selected"} {
		got, err := WithBrowserMCP(command, "/path with space/mewla", "/private/state", false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, command+" ") || !strings.Contains(got, "mewla_browser") || strings.Contains(got, "--strict-mcp-config") || strings.Contains(got, "--task") {
			t.Fatal(got)
		}
		if _, ok := shellquote.Fields(got); !ok {
			t.Fatalf("unsafe command: %s", got)
		}
	}
	for _, command := range []string{"sh", "codex; echo bad", "codex --", "codex -c mcp_servers.mewla_browser={}"} {
		if _, err := WithBrowserMCP(command, "/mewla", "/state", false); err == nil {
			t.Fatalf("accepted %s", command)
		}
	}
}
