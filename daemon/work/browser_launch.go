package work

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/google/uuid"
)

// WithBrowserMCP adds only an invocation-scoped MCP entry. Global provider
// configuration and other MCP servers are preserved. The managed launch shell
// supplies MEWLA_WORKER_ID after tmux allocates the actual session identity.
func WithBrowserMCP(command, executable, stateDir, resourceID string) (string, error) {
	if _, err := uuid.Parse(resourceID); err != nil {
		return "", fmt.Errorf("invalid Browser resource")
	}
	opts, ok := inspectLaunchCommandOptions(command)
	if !ok || opts.terminated {
		return "", fmt.Errorf("Browser attachment requires a direct provider command without an existing mewla_browser override")
	}
	for _, arg := range opts.argv {
		if strings.HasPrefix(arg, "mcp_servers.mewla_browser") || arg == "--mcp-config" || strings.HasPrefix(arg, "--mcp-config=") {
			return "", fmt.Errorf("Browser attachment owns the invocation MCP entry; remove a conflicting command-level MCP override")
		}
	}
	args := []string{"browser", "mcp", "--id", resourceID, "--state-dir", stateDir}
	switch InferWorkerProvider(command) {
	case WorkerProviderCodex:
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = strconv.Quote(arg)
		}
		config := "mcp_servers.mewla_browser={command=" + strconv.Quote(executable) + ",args=[" + strings.Join(quoted, ",") + "],enabled=true}"
		return command + " -c " + shellquote.Word(config), nil
	case WorkerProviderClaude:
		raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"mewla_browser": map[string]any{"command": executable, "args": args}}})
		return command + " --mcp-config " + shellquote.Word(string(raw)) + " --allowedTools mcp__mewla_browser__browser", nil
	default:
		return "", fmt.Errorf("Browser attachment currently supports Codex and Claude sessions; choose either or launch without a Browser")
	}
}
