package work

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

// agentLookPath finds an agent's program. It is the one lookup behind every
// answer to "is this agent installed here": Brain's executor states (the host
// picker and the New session sheet's tiles) and the refusal of a launch, so a
// tile and its refusal never disagree. Tests replace it with SetAgentLookPath.
var agentLookPath = hostdeps.LookPath

// AgentLookPath looks an agent's program up the way a Session's shell would:
// the daemon's PATH, then the installer and per-user directories.
func AgentLookPath(name string) (string, error) { return agentLookPath(name) }

// SetAgentLookPath replaces AgentLookPath and returns the function that
// restores it.
func SetAgentLookPath(look func(name string) (string, error)) (restore func()) {
	previous := agentLookPath
	agentLookPath = look
	return func() { agentLookPath = previous }
}

// agentProductNames are the products behind the agent providers.
var agentProductNames = map[string]string{
	WorkerProviderClaude:      "Claude Code",
	WorkerProviderCodex:       "Codex",
	WorkerProviderCursor:      "Cursor Agent",
	WorkerProviderGrok:        "Grok",
	WorkerProviderPi:          "Pi",
	WorkerProviderOpenCode:    "OpenCode",
	WorkerProviderAntigravity: "Antigravity",
	WorkerProviderDSH:         "DSH",
}

// AgentInstallCommand is the line that installs an agent's CLI on goos, for the
// agents Mewla has an official one for; "" for any other.
func AgentInstallCommand(provider, goos string) string {
	if provider == WorkerProviderAntigravity {
		// Google's official installer for the Antigravity CLI
		// (antigravity.google/download): a per-user install, in ~/.local/bin on
		// macOS and Linux and in %LOCALAPPDATA%\agy\bin on Windows (PowerShell).
		if goos == "windows" {
			return "irm https://antigravity.google/cli/install.ps1 | iex"
		}
		return "curl -fsSL https://antigravity.google/cli/install.sh | bash"
	}
	for _, agent := range hostdeps.Agents(hostdeps.Platform{GOOS: goos}) {
		if agent.ID == provider {
			return agent.Install.Command()
		}
	}
	return ""
}

// MissingAgent is a known agent's program that is not on this computer.
type MissingAgent struct {
	Provider string
	Name     string
	Program  string
	// Install is the line that installs it, when Mewla has one.
	Install string
}

// Message is the cause in one plain sentence, without the install command
// (that travels as its own field, for the CLI and for an Install action).
func (m MissingAgent) Message() string {
	return fmt.Sprintf("%s (%s) is not installed on this computer.", m.Name, m.Program)
}

// MessageWithInstall is the cause for a terminal: the sentence and the line
// that installs it, when Mewla has one.
func (m MissingAgent) MessageWithInstall() string {
	if m.Install == "" {
		return m.Message()
	}
	return m.Message() + " Install it with: " + m.Install
}

// launchProgram is the program a launch command runs: the first word, after
// "env" and NAME=value words.
func launchProgram(command string) string {
	fields := strings.Fields(command)
	if len(fields) > 0 && fields[0] == "env" {
		fields = fields[1:]
	}
	for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "-") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"'`)
}

// LaunchProgramProvider is the agent a command line starts by its program
// ("claude --model haiku" is Claude; "zsh -i -l -c 'claude …'" is a shell that
// has not started Claude yet): "" for any other program.
func LaunchProgramProvider(command string) string {
	program := launchProgram(command)
	if program == "" {
		return ""
	}
	return inferWorkerProviderOne(filepath.Base(program))
}

// MissingAgentFor says whether a launch command runs a known agent's CLI that
// is not installed here (nil when it is, and for a shell, which has no command,
// and for any other program: a custom command is the person's own). A Session
// started on a missing agent only ends at "exit 127", so a launch is refused
// before it exists, with this cause.
func MissingAgentFor(command string) *MissingAgent {
	program := launchProgram(command)
	if program == "" || filepath.Base(program) == "cc" {
		return nil
	}
	provider := inferWorkerProviderOne(filepath.Base(program))
	name, known := agentProductNames[provider]
	if !known {
		return nil
	}
	if _, err := AgentLookPath(program); err == nil {
		return nil
	}
	return &MissingAgent{Provider: provider, Name: name, Program: filepath.Base(program), Install: AgentInstallCommand(provider, runtime.GOOS)}
}
