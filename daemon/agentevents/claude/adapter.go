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
	// Hook is what that file runs. A launch that already carries inline
	// --settings JSON (a routed Session's env) gets the same hooks merged into
	// it: Claude reads only one --settings.
	Hook Hook
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
// is known from the first moment, and the hooks. A command that already names
// or continues a session keeps it. Claude reads only one --settings, so a
// command whose --settings is inline JSON (the route env and permission mode
// Mewla gives every routed Session) gets the hooks merged into that JSON; one
// that names its own settings file, or whose JSON brings its own hooks or
// turns hooks off, is left as it is, so the user's choice stands and that
// Worker is followed by the fallback.
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	command, err := work.EnsureClaudeSessionLaunchCommand(command)
	if err != nil {
		return agentevents.Launch{}, err
	}
	if a.SettingsPath == "" {
		return agentevents.Launch{Command: command}, nil
	}
	if !hasSettingsFlag(command) {
		return agentevents.Launch{Command: strings.TrimSpace(command) + " --settings " + shellquote.Word(a.SettingsPath)}, nil
	}
	if merged, ok := a.withInlineHooks(command); ok {
		return agentevents.Launch{Command: merged}, nil
	}
	return agentevents.Launch{Command: command}, nil
}

func hasSettingsFlag(command string) bool {
	fields, ok := shellquote.Fields(command)
	if !ok {
		fields = strings.Fields(command)
	}
	for _, field := range fields {
		if field == "--settings" || strings.HasPrefix(field, "--settings=") {
			return true
		}
	}
	return false
}

// withInlineHooks merges the hooks into the command's one inline --settings
// JSON object, keeping every other byte of the command as written.
func (a Adapter) withInlineHooks(command string) (string, bool) {
	if a.Hook.Command == "" {
		return "", false
	}
	fields, ok := shellquote.Fields(command)
	if !ok {
		return "", false
	}
	index, prefix, raw := -1, "", ""
	for i := 1; i < len(fields); i++ {
		field := fields[i]
		if field == "--" {
			break
		}
		value, isSettings := "", false
		switch {
		case field == "--settings" && i+1 < len(fields):
			value, isSettings = fields[i+1], true
			i++
		case strings.HasPrefix(field, "--settings="):
			value, isSettings = strings.TrimPrefix(field, "--settings="), true
		}
		if !isSettings {
			continue
		}
		if index >= 0 {
			// Which one Claude keeps is not ours to guess.
			return "", false
		}
		if field == "--settings" {
			index, prefix = i, ""
		} else {
			index, prefix = i, "--settings="
		}
		raw = value
	}
	if index < 0 {
		return "", false
	}
	var settings map[string]json.RawMessage
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") || json.Unmarshal([]byte(raw), &settings) != nil || settings == nil {
		return "", false
	}
	if _, own := settings["hooks"]; own {
		return "", false
	}
	if _, off := settings["disableAllHooks"]; off {
		return "", false
	}
	file, err := HookSettingsJSON(a.Hook)
	if err != nil {
		return "", false
	}
	var hooked map[string]json.RawMessage
	if json.Unmarshal(file, &hooked) != nil {
		return "", false
	}
	for key, value := range hooked {
		if _, set := settings[key]; !set {
			settings[key] = value
		}
	}
	merged, err := json.Marshal(settings)
	if err != nil {
		return "", false
	}
	out, ok := shellquote.ReplaceFields(command, map[int]string{index: prefix + string(merged)})
	if !ok {
		return "", false
	}
	return out, true
}

// Hook is what Claude runs for each hook (it receives the payload on stdin):
// a shell command line, or with Args a program run directly with those
// arguments and no shell (Claude's exec form). Windows uses the exec form:
// there Claude runs a command line in PowerShell, or Git Bash when installed,
// and no one quoting reads the same in both.
type Hook struct {
	Command string
	Args    []string
}

// SettingsJSON is the hook file for hookCommand, a shell command line.
func SettingsJSON(hookCommand string) ([]byte, error) {
	return HookSettingsJSON(Hook{Command: hookCommand})
}

// HookSettingsJSON is the hook file for hook. PermissionRequest may wait on
// Brain or a person, so it gets the longest timeout Claude allows by default.
// The file also acknowledges Claude's one-time bypass-mode warning, which
// every Mewla-launched Claude starts in, so a first Worker on a fresh install
// is never left at that dialog.
func HookSettingsJSON(hook Hook) ([]byte, error) {
	hooks := map[string]any{}
	for _, name := range HookEvents {
		entry := map[string]any{"type": "command", "command": hook.Command}
		if hook.Args != nil {
			entry["args"] = hook.Args
		}
		if name == "PermissionRequest" {
			entry["timeout"] = 600
		} else {
			entry["timeout"] = 10
		}
		hooks[name] = []any{map[string]any{"hooks": []any{entry}}}
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks, "skipDangerousModePermissionPrompt": true}, "", "  ")
}

// WriteSettings writes the hook file for a shell command line.
func WriteSettings(path, hookCommand string) error {
	return WriteHookSettings(path, Hook{Command: hookCommand})
}

// WriteHookSettings writes the hook file when its content changed, so
// concurrent launches never see a half-written file.
func WriteHookSettings(path string, hook Hook) error {
	want, err := HookSettingsJSON(hook)
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
