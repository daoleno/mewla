// Package cursor gives a Cursor Agent Worker its conversation from the first
// moment.
//
// Cursor's terminal UI chooses its chat id itself and offers Mewla no event
// stream, so Mewla used to find the Worker's transcript afterwards by folder
// and start time, which two Workers in one folder can confuse. Cursor can
// create an empty chat first (`cursor-agent create-chat`) and resume it, and
// the resumed conversation keeps that id, so the Worker's transcript is known
// at launch. Turns, messages and permission prompts are still read from the
// transcript and the screen: Cursor's hooks can only be installed in the
// user's own settings or the project.
package cursor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/work"
)

// Provider is the executor provider this adapter serves.
const Provider = "cursor"

const createChatTimeout = 20 * time.Second

// subcommands of the cursor-agent CLI: a launch that runs one is not an
// interactive chat and is left alone.
var subcommands = map[string]bool{
	"persist": true, "install-shell-integration": true, "uninstall-shell-integration": true, "login": true,
	"logout": true, "mcp": true, "plugin": true, "worker": true, "status": true, "whoami": true, "models": true,
	"team": true, "bedrock": true, "about": true, "update": true, "create-chat": true, "generate-rule": true,
	"rule": true, "ls": true, "resume": true, "help": true,
}

// Adapter gives each fresh Cursor launch a chat of its own.
type Adapter struct {
	// NewChat creates an empty chat with the given cursor-agent program and
	// returns its id. Nil uses the installed CLI.
	NewChat func(ctx context.Context, program string) (string, error)
}

// Descriptor declares what Mewla knows about a Cursor Worker: its conversation
// from launch, and nothing pushed.
func (Adapter) Descriptor() agentevents.Descriptor {
	return agentevents.Descriptor{
		Provider: Provider,
		Capabilities: map[agentevents.Capability]agentevents.Level{
			agentevents.CapBinding:   agentevents.LevelStructured,
			agentevents.CapEvents:    agentevents.LevelFile,
			agentevents.CapSend:      agentevents.LevelKeystroke,
			agentevents.CapApprove:   agentevents.LevelKeystroke,
			agentevents.CapInterrupt: agentevents.LevelKeystroke,
		},
	}
}

// PrepareLaunch makes the launch resume a chat created for it. A launch that
// already resumes or continues a chat, runs a subcommand, or cannot be read as
// a plain command is left as it is, as is one when no chat can be created.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 || work.InferWorkerProvider(command) != work.WorkerProviderCursor {
		return agentevents.Launch{Command: command}, nil
	}
	for _, field := range fields[1:] {
		if field == "--continue" || field == "-c" || subcommands[field] {
			return agentevents.Launch{Command: command}, nil
		}
	}
	if _, present, err := work.ProviderResumeToken(work.WorkerProviderCursor, command); err != nil || present {
		return agentevents.Launch{Command: command}, nil
	}
	newChat := a.NewChat
	if newChat == nil {
		newChat = createChat
	}
	ctx, cancel := context.WithTimeout(context.Background(), createChatTimeout)
	defer cancel()
	id, err := newChat(ctx, strings.Trim(fields[0], `"'`))
	if err != nil || !validChatID(id) {
		return agentevents.Launch{Command: command}, nil
	}
	return agentevents.Launch{Command: strings.TrimSpace(command) + " --resume " + shellquote.Word(id)}, nil
}

func validChatID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f'):
			return false
		}
	}
	return true
}

// createChat runs `<program> create-chat`, which prints the new chat's id.
func createChat(ctx context.Context, program string) (string, error) {
	path := program
	if filepath.Base(program) == program {
		found, err := hostdeps.LookPath(program)
		if err != nil {
			return "", err
		}
		path = found
	}
	out, err := exec.CommandContext(ctx, path, "create-chat").Output()
	if err != nil {
		return "", fmt.Errorf("cursor-agent create-chat: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}
