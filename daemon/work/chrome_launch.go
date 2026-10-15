package work

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// ChromeMCPName is the agent-facing name of the user's own Chrome.
const ChromeMCPName = "mewla_chrome"

// WithChromeMCP gives a Codex or Claude launch the user's Chrome as an
// invocation-scoped MCP server. Anything else, or a command that already
// names MCP servers itself, is returned unchanged: the user's Chrome is an
// extra, never a reason a launch fails.
func WithChromeMCP(command, executable, stateDir string) string {
	opts, ok := inspectLaunchCommandOptions(command)
	if !ok || opts.terminated {
		return command
	}
	for _, arg := range opts.argv {
		if strings.HasPrefix(arg, "mcp_servers."+ChromeMCPName) || arg == "--mcp-config" || strings.HasPrefix(arg, "--mcp-config=") || arg == "--strict-mcp-config" {
			return command
		}
	}
	args := []string{"agent-use", "chrome-mcp", "--state-dir", stateDir}
	switch InferWorkerProvider(command) {
	case WorkerProviderCodex:
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = strconv.Quote(arg)
		}
		config := "mcp_servers." + ChromeMCPName + "={command=" + strconv.Quote(executable) + ",args=[" + strings.Join(quoted, ",") + "],enabled=true}"
		return command + " -c " + shellquote.Word(config)
	case WorkerProviderClaude:
		raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{ChromeMCPName: map[string]any{"command": executable, "args": args}}})
		return command + " --mcp-config " + shellquote.Word(string(raw)) + " --allowedTools mcp__" + ChromeMCPName
	default:
		return command
	}
}
