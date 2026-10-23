// Package pi adapts Pi to the normalized agent events through an extension.
//
// Pi's terminal UI has no event stream or control channel, but Pi runs
// extensions in its own process. Mewla loads one into each Pi it starts
// (pi -e); the extension reports Pi's lifecycle to the daemon over a local
// socket and takes a message or a stop back. The session file is chosen at
// launch, so the conversation is known from the first moment.
package pi

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/work"
)

// Provider is the executor provider this adapter serves.
const Provider = "pi"

//go:embed extension.js
var extensionSource string

// Adapter follows a Pi Worker through the extension at ExtensionPath.
type Adapter struct {
	ExtensionPath string
}

// Descriptor declares Pi's capabilities. The extension is the agent calling
// out to Mewla, so events, sending and stopping are hook level; Pi has no
// permission prompts to answer.
func (Adapter) Descriptor() agentevents.Descriptor {
	return agentevents.Descriptor{
		Provider: Provider,
		Capabilities: map[agentevents.Capability]agentevents.Level{
			agentevents.CapBinding:   agentevents.LevelStructured,
			agentevents.CapEvents:    agentevents.LevelHook,
			agentevents.CapSend:      agentevents.LevelHook,
			agentevents.CapApprove:   agentevents.LevelNone,
			agentevents.CapInterrupt: agentevents.LevelHook,
		},
	}
}

// PrepareLaunch gives a fresh launch its own session file and loads the
// extension. A launch Mewla cannot read as a plain command, or one that opts
// out of sessions, is left as it is and followed by the fallback.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	prepared, err := work.EnsurePiSessionLaunchCommand(command)
	if err != nil {
		return agentevents.Launch{Command: command}, nil
	}
	if a.ExtensionPath == "" || work.InferWorkerProvider(prepared) != work.WorkerProviderPi {
		return agentevents.Launch{Command: prepared}, nil
	}
	if _, present, err := work.ProviderResumeToken(work.WorkerProviderPi, prepared); err != nil || !present {
		return agentevents.Launch{Command: command}, nil
	}
	return agentevents.Launch{Command: strings.TrimSpace(prepared) + " -e " + shellquote.Word(a.ExtensionPath)}, nil
}

// WriteExtension writes the extension, pointed at the daemon's socket, when its
// content changed, so concurrent launches never see a half-written file.
func WriteExtension(path, socket string) error {
	quoted, err := json.Marshal(socket)
	if err != nil {
		return err
	}
	want := strings.Replace(extensionSource, "__MEWLA_SOCKET__", string(quoted), 1)
	if have, err := os.ReadFile(path); err == nil && string(have) == want {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pi-extension-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(want); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
