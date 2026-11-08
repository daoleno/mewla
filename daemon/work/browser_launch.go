package work

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// WithBrowserMCP adds only an invocation-scoped MCP entry. Global provider
// configuration and other MCP servers are preserved. The managed launch shell
// supplies MEWLA_WORKER_ID after tmux allocates the actual session identity.
func WithBrowserMCP(command, executable, stateDir string, withDisplay bool) (string, error) {
	opts, ok := inspectLaunchCommandOptions(command)
	if !ok || opts.terminated {
		return "", fmt.Errorf("Browser attachment requires a direct provider command without an existing mewla_browser override")
	}
	for _, arg := range opts.argv {
		if strings.HasPrefix(arg, "mcp_servers.mewla_browser") || arg == "--mcp-config" || strings.HasPrefix(arg, "--mcp-config=") {
			return "", fmt.Errorf("Browser attachment owns the invocation MCP entry; remove a conflicting command-level MCP override")
		}
	}
	args := []string{"browser", "mcp", "--state-dir", stateDir}
	switch InferWorkerProvider(command) {
	case WorkerProviderCodex:
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = strconv.Quote(arg)
		}
		config := "mcp_servers.mewla_browser={command=" + strconv.Quote(executable) + ",args=[" + strings.Join(quoted, ",") + "],enabled=true}"
		command += " -c " + shellquote.Word(config)
		if withDisplay {
			command += " -c " + shellquote.Word(displayServerConfig(executable, stateDir))
		}
		return command, nil
	case WorkerProviderClaude:
		servers := map[string]any{"mewla_browser": map[string]any{"command": executable, "args": args}}
		allowed := "mcp__mewla_browser__browser"
		if withDisplay {
			servers[DisplayMCPName] = map[string]any{"command": executable, "args": displayServerArgs(stateDir)}
			allowed += ",mcp__" + DisplayMCPName
		}
		raw, _ := json.Marshal(map[string]any{"mcpServers": servers})
		return command + " --mcp-config " + shellquote.Word(string(raw)) + " --allowedTools " + allowed, nil
	default:
		return "", fmt.Errorf("Browser attachment currently supports Codex and Claude sessions")
	}
}

// SupportsBrowserAttachment is whether a launch command is a provider that
// can take the Browser MCP entry (Codex and Claude).
func SupportsBrowserAttachment(command string) bool {
	switch InferWorkerProvider(command) {
	case WorkerProviderCodex, WorkerProviderClaude:
		return true
	}
	return false
}

func displayServerArgs(stateDir string) []string {
	return []string{"agent-use", "display-mcp", "--state-dir", stateDir}
}

// displayServerConfig is the Codex config line for the display server; it
// needs the Worker's identity from the launch environment.
func displayServerConfig(executable, stateDir string) string {
	return "mcp_servers." + DisplayMCPName + "={command=" + strconv.Quote(executable) + ",args=" + tomlStrings(displayServerArgs(stateDir)) + ",enabled=true,env_vars=" + tomlStrings([]string{"MEWLA_WORKER_ID"}) + "}"
}
