package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/work"
)

// Provider is the executor provider this adapter serves.
const Provider = "claude"

// Adapter follows a Claude Worker through the hooks in SettingsPath.
type Adapter struct {
	// SettingsPath is the hook file passed to each launch with --settings.
	SettingsPath string
}

// Descriptor declares Claude's capabilities at the levels the hooks give.
// Sending input and interrupting have no supported typed interface for the
// interactive TUI, so they stay keystrokes; the hook still proves delivery.
func (Adapter) Descriptor() agentevents.Descriptor {
	return agentevents.Descriptor{
		Provider: Provider,
		Capabilities: map[agentevents.Capability]agentevents.Level{
			agentevents.CapBinding:   agentevents.LevelStructured,
			agentevents.CapEvents:    agentevents.LevelHook,
			agentevents.CapSend:      agentevents.LevelKeystroke,
			agentevents.CapApprove:   agentevents.LevelHook,
			agentevents.CapInterrupt: agentevents.LevelKeystroke,
		},
	}
}

// PrepareLaunch gives a fresh launch its own session id, so its conversation
// is known from the first moment, and the hook file. A command that already
// names or continues a session keeps it, and one that carries its own
// --settings is left without the hook file, so the user's choice stands and
// that Worker is followed by the fallback.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	command, err := work.EnsureClaudeSessionLaunchCommand(command)
	if err != nil {
		return agentevents.Launch{}, err
	}
	if a.SettingsPath == "" || hasSettingsFlag(command) {
		return agentevents.Launch{Command: command}, nil
	}
	return agentevents.Launch{Command: strings.TrimSpace(command) + " --settings " + shellquote.Word(a.SettingsPath)}, nil
}

func hasSettingsFlag(command string) bool {
	for _, field := range strings.Fields(command) {
		if field == "--settings" || strings.HasPrefix(field, "--settings=") {
			return true
		}
	}
	return false
}

// SettingsJSON is the hook file for hookCommand, the command line Claude runs
// for each hook (it receives the payload on stdin). PermissionRequest may wait
// on a person, so it gets the longest timeout Claude allows by default.
func SettingsJSON(hookCommand string) ([]byte, error) {
	hooks := map[string]any{}
	for _, name := range HookEvents {
		entry := map[string]any{"type": "command", "command": hookCommand}
		if name == "PermissionRequest" {
			entry["timeout"] = 600
		} else {
			entry["timeout"] = 10
		}
		hooks[name] = []any{map[string]any{"hooks": []any{entry}}}
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
}

// WriteSettings writes the hook file when its content changed, so concurrent
// launches never see a half-written file.
func WriteSettings(path, hookCommand string) error {
	want, err := SettingsJSON(hookCommand)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(want) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(want); err != nil {
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
