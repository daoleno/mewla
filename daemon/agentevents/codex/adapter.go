package codex

import (
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/codextrust"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/google/uuid"
)

// Adapter follows a Codex Worker through an app-server of its own.
type Adapter struct {
	// ControlDir is where each launch's control socket, pid and log live.
	ControlDir string
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
		},
	}
}

// PrepareLaunch runs the TUI against an app-server started beside it, so the
// server can be watched as a second client. Only a plain interactive launch
// (flags, no subcommand) is wrapped; a launch that already talks to a server,
// is a subcommand, or has no control directory is left as it is and followed
// by the fallback.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	rest, ok := plainInteractiveArgs(command)
	if !ok || a.ControlDir == "" {
		return agentevents.Launch{Command: command}, nil
	}
	socket := filepath.Join(a.ControlDir, "codex-ctl-"+uuid.NewString()+".sock")
	program := strings.Fields(command)[0]
	// The app-server is the process that loads the folder's own config, so it
	// carries the launch's folder trust; the attached TUI shows no trust screen.
	server := program + " app-server --listen unix://" + shellquote.Word(socket) + codextrust.Forwarded(rest)
	log, pid := ControlLogPath(socket), ControlPidPath(socket)
	wrapped := "set +m; " + server + " > " + shellquote.Word(log) + " 2>&1 & echo $! > " + shellquote.Word(pid) +
		"; exec " + program + " --remote unix://" + shellquote.Word(socket)
	if rest != "" {
		wrapped += " " + rest
	}
	return agentevents.Launch{Command: wrapped, Control: socket}, nil
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
