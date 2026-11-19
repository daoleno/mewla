package codex

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/codextrust"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// Adapter follows a Codex Worker through an app-server of its own.
type Adapter struct {
	// ControlDir is where each launch's control socket, pid and log live.
	ControlDir string
	// Inline starts the TUI inline (InlineTUI) unless the person's Codex
	// config at ConfigPath chose the screen.
	Inline     bool
	ConfigPath string
	// Mewla is the mewla program. A wrapped launch runs its
	// `codex-server-wait` before the TUI, so the TUI never connects to an
	// app-server that is not listening yet. Empty leaves the wait out.
	Mewla string
}

// Descriptor declares Codex's capabilities: the app-server is a typed
// interface for everything, so nothing is a keystroke.
func (Adapter) Descriptor() agentevents.Descriptor {
	return agentevents.Descriptor{
		Provider: Provider,
		Capabilities: map[agentevents.Capability]agentevents.Level{
			agentevents.CapBinding:   agentevents.LevelStructured,
			agentevents.CapEvents:    agentevents.LevelStructured,
			agentevents.CapSend:      agentevents.LevelStructured,
			agentevents.CapApprove:   agentevents.LevelStructured,
			agentevents.CapInterrupt: agentevents.LevelStructured,
			// thread/backgroundTerminals/terminate, for the turn's own items.
			agentevents.CapStopCommands: agentevents.LevelStructured,
		},
	}
}

// PrepareLaunch runs the TUI against an app-server started beside it, so the
// server can be watched as a second client. Only a plain interactive launch
// (flags, no subcommand) is wrapped; a launch that already talks to a server,
// is a subcommand, or has no control directory is left as it is and followed
// by the fallback.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	if a.Inline {
		command = InlineTUI(command, ReadConfig(a.ConfigPath))
	}
	rest, ok := plainInteractiveArgs(command)
	if !ok || a.ControlDir == "" {
		return agentevents.Launch{Command: command}, nil
	}
	// A long state directory would put the socket past what a unix socket
	// can name; platform.SocketPath gives it a short name then.
	socket := platform.SocketPath(filepath.Join(a.ControlDir, "codex-ctl-"+uuid.NewString()+".sock"))
	if err := platform.EnsureSocketDir(socket); err != nil {
		return agentevents.Launch{Command: command}, nil
	}
	program := strings.Fields(command)[0]
	// The app-server is the process that loads the folder's own config, so it
	// carries the launch's folder trust; the attached TUI shows no trust screen.
	// It also starts the MCP servers, so the tools Mewla adds to the launch
	// (mewla_chrome, mewla_computer) are forwarded too.
	server := program + " app-server --listen unix://" + shellquote.Word(socket) + codextrust.Forwarded(rest) + forwardedMCP(rest)
	log, pid := ControlLogPath(socket), ControlPidPath(socket)
	wrapped := server + " > " + shellquote.Word(log) + " 2>&1 & echo $! > " + shellquote.Word(pid) + "; "
	// `codex --remote` tries to connect once and exits 1 if the app-server
	// is not listening yet, so the TUI starts only once it is.
	if a.Mewla != "" {
		wrapped += shellquote.Word(a.Mewla) + " codex-server-wait " + shellquote.Word(socket) + "; "
	}
	wrapped += "exec " + program + " --remote unix://" + shellquote.Word(socket)
	// Keep the server in the pane's process group. Windows launches through
	// `mewla sh`, which has no job control and no -m option.
	if runtime.GOOS != "windows" {
		wrapped = "set +m; " + wrapped
	}
	if rest != "" {
		wrapped += " " + rest
	}
	return agentevents.Launch{Command: wrapped, Control: socket}, nil
}

// forwardedMCP returns, as launch options for the app-server, the MCP server
// overrides (-c mcp_servers.<name>=...) that the TUI command line carries.
func forwardedMCP(tuiArgs string) string {
	fields, ok := shellquote.Fields(tuiArgs)
	if !ok {
		return ""
	}
	var out strings.Builder
	for index := 0; index+1 < len(fields); index++ {
		if (fields[index] == "--config" || fields[index] == "-c") && strings.HasPrefix(fields[index+1], "mcp_servers.") {
			out.WriteString(" -c " + shellquote.Word(fields[index+1]))
		}
	}
	return out.String()
}

// ControlPidPath is where the launch wrapper records the app-server's pid.
func ControlPidPath(socket string) string { return strings.TrimSpace(socket) + ".pid" }

// ControlLogPath is the app-server's log file.
func ControlLogPath(socket string) string {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return ""
	}
	return strings.TrimSuffix(socket, filepath.Ext(socket)) + ".log"
}

// plainInteractiveArgs returns the arguments after the program when the
// command is `codex [flags]` and nothing else: no environment prefix, no
// subcommand, no shell syntax, and no existing --remote.
func plainInteractiveArgs(command string) (rest string, ok bool) {
	command = strings.TrimSpace(command)
	fields, ok := shellquote.Fields(command)
	if !ok || len(fields) == 0 || filepath.Base(fields[0]) != "codex" {
		return "", false
	}
	if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") {
		return "", false
	}
	for _, field := range fields[1:] {
		if field == "--remote" || strings.HasPrefix(field, "--remote=") || field == "app-server" {
			return "", false
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(command, strings.Fields(command)[0])), true
}
