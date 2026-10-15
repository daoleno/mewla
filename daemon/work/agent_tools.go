package work

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// Agent-facing names of this computer's tools.
const (
	ChromeMCPName   = "mewla_chrome"
	ComputerMCPName = "mewla_computer"
	// CodexComputerUseName is Codex's own computer use, from the user's
	// Codex config.
	CodexComputerUseName = "computer-use"
)

// AgentTools is what an agent launched now can use on this computer.
type AgentTools struct {
	// Chrome is the user's own Chrome, through the Playwright extension.
	Chrome bool
	// Computer is this computer's screen, keyboard and pointer.
	Computer bool
	// NoCodexComputerUse turns off Codex's own computer use, which the
	// user's Codex config adds, while agents may not use this computer.
	NoCodexComputerUse bool
}

// WithAgentTools gives a Codex or Claude launch this computer's ready tools
// as invocation-scoped MCP servers. Anything else, or a command that already
// names MCP servers itself, is returned unchanged: these tools are extras,
// never a reason a launch fails. A Codex launch can also have Codex's own
// computer use turned off.
func WithAgentTools(command, executable, stateDir string, tools AgentTools) string {
	type server struct {
		name string
		args []string
		// env names the variables the server needs from the agent's own
		// environment (Codex passes MCP servers only a short list).
		env []string
	}
	var servers []server
	if tools.Chrome {
		servers = append(servers, server{ChromeMCPName, []string{"agent-use", "chrome-mcp", "--state-dir", stateDir}, nil})
	}
	if tools.Computer {
		servers = append(servers, server{ComputerMCPName, []string{"agent-use", "computer-mcp", "--state-dir", stateDir}, []string{"MEWLA_WORKER_ID"}})
	}
	// Only a config that defines the server can turn it off: Codex refuses a
	// server entry with nothing to run. A launch that picks its own Codex
	// home may not have it, so it is left alone.
	disableCodex := tools.NoCodexComputerUse && InferWorkerProvider(command) == WorkerProviderCodex && !strings.Contains(command, "CODEX_HOME=")
	if len(servers) == 0 && !disableCodex {
		return command
	}
	opts, ok := inspectLaunchCommandOptions(command)
	if !ok || opts.terminated {
		return command
	}
	for _, arg := range opts.argv {
		if strings.HasPrefix(arg, "mcp_servers."+ChromeMCPName) || strings.HasPrefix(arg, "mcp_servers."+ComputerMCPName) ||
			arg == "--mcp-config" || strings.HasPrefix(arg, "--mcp-config=") || arg == "--strict-mcp-config" {
			return command
		}
		if strings.HasPrefix(arg, "mcp_servers."+CodexComputerUseName) {
			disableCodex = false
		}
	}
	if disableCodex {
		command += " -c " + shellquote.Word("mcp_servers."+CodexComputerUseName+".enabled=false")
	}
	switch InferWorkerProvider(command) {
	case WorkerProviderCodex:
		for _, s := range servers {
			config := "mcp_servers." + s.name + "={command=" + strconv.Quote(executable) + ",args=" + tomlStrings(s.args) + ",enabled=true"
			if len(s.env) > 0 {
				config += ",env_vars=" + tomlStrings(s.env)
			}
			command += " -c " + shellquote.Word(config+"}")
		}
		return command
	case WorkerProviderClaude:
		entries := map[string]any{}
		allowed := make([]string, 0, len(servers))
		for _, s := range servers {
			entries[s.name] = map[string]any{"command": executable, "args": s.args}
			allowed = append(allowed, "mcp__"+s.name)
		}
		raw, _ := json.Marshal(map[string]any{"mcpServers": entries})
		return command + " --mcp-config " + shellquote.Word(string(raw)) + " --allowedTools " + strings.Join(allowed, ",")
	default:
		return command
	}
}

func tomlStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
