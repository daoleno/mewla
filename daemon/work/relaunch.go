package work

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// RelaunchCommand is the launch that starts a delegated Worker's agent again
// in its conversation: launch is the command the Worker was first started
// with (before Mewla's tools and adapters wrap it), token the conversation it
// is in now. It is one path for every agent; WithProviderResumeToken holds
// each one's resume shape:
//
//	claude --session-id X   → claude --resume <token> (the fresh id, or the newer
//	                          one a /clear started, takes Claude's resume flag)
//	codex …                 → codex … resume <token>
//	agy …                   → agy … --conversation <token>
//	cursor-agent / grok …   → … --resume <token>
//	opencode … --session S  → unchanged (OpenCode continues S)
//	pi … --session P        → unchanged (Pi continues P)
//	dsh                     → mewla dsh-session --dsh-session <token>
func RelaunchCommand(provider, launch, token string) (string, error) {
	launch, token = strings.TrimSpace(launch), strings.TrimSpace(token)
	if launch == "" || token == "" {
		return "", fmt.Errorf("a relaunch needs the launch and its conversation")
	}
	provider = normalizeResumeProvider(provider, launch)
	if provider == WorkerProviderCodex {
		command, err := CodexNativeResumeCommand(launch, token)
		if err != nil {
			return "", err
		}
		return codexResumeSubcommandFirst(command, token)
	}
	existing, present, err := ProviderResumeToken(provider, launch)
	if err != nil {
		return "", err
	}
	if present && existing != token {
		if provider != WorkerProviderClaude {
			return "", fmt.Errorf("the launch names conversation %q, not %q", existing, token)
		}
		// The conversation moved on (a /clear): resume the newer one.
		if launch, err = withoutClaudeSessionDeclaration(launch); err != nil {
			return "", err
		}
	}
	return WithProviderResumeToken(provider, launch, token)
}

// codexResumeSubcommandFirst moves `resume <token>` right after the program:
// `codex resume <id> --flags…`. A resume through Mewla's app-server wrap
// (`codex --remote … resume`) refuses the Worker's permission flags ("Permission
// overrides are not supported when resuming a remote task"), and the launch
// adapter leaves a subcommand launch unwrapped, as the Brain host's own Codex
// resume runs.
func codexResumeSubcommandFirst(command, token string) (string, error) {
	fields, ok := shellquote.Fields(command)
	if !ok || len(fields) < 3 {
		return "", ErrLaunchUnparseable
	}
	out := []string{fields[0], "resume", token}
	for index := 1; index < len(fields); index++ {
		if fields[index] == "resume" && index+1 < len(fields) && fields[index+1] == token {
			index++
			continue
		}
		out = append(out, fields[index])
	}
	for index := range out {
		out[index] = shellquote.Word(out[index])
	}
	return strings.Join(out, " "), nil
}

// withoutClaudeSessionDeclaration drops --session-id/--resume/-r and its value.
func withoutClaudeSessionDeclaration(command string) (string, error) {
	fields, ok := shellquote.Fields(command)
	if !ok {
		return "", ErrLaunchUnparseable
	}
	out := make([]string, 0, len(fields))
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		if field == "--" {
			out = append(out, fields[index:]...)
			break
		}
		switch {
		case field == "--session-id" || field == "--resume" || field == "-r":
			index++
			continue
		case strings.HasPrefix(field, "--session-id=") || strings.HasPrefix(field, "--resume="):
			continue
		}
		out = append(out, field)
	}
	for index := range out {
		out[index] = shellquote.Word(out[index])
	}
	return strings.Join(out, " "), nil
}

// AgentProductName names the agent a launch runs ("Claude Code", "Codex");
// a custom program is named by its executable.
func AgentProductName(provider, command string) string {
	if name, ok := agentProductNames[normalizeResumeProvider(provider, command)]; ok {
		return name
	}
	if program := filepath.Base(launchProgram(command)); program != "" && program != "." {
		return program
	}
	return "The agent"
}

// ProgramUpdatedBetween reports whether the program a launch runs was
// replaced on disk after start and no later than end: the agent was updated
// while it ran, before it died. A change after end says nothing about the
// death. It looks at the program as the shell finds it and, for a link, at the
// link and at what it points to.
func ProgramUpdatedBetween(command string, start, end time.Time) bool {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return false
	}
	within := func(at time.Time) bool { return at.After(start) && !at.After(end) }
	program := launchProgram(command)
	if program == "" {
		return false
	}
	path, err := AgentLookPath(program)
	if err != nil {
		return false
	}
	if info, err := os.Lstat(path); err == nil && within(info.ModTime()) {
		return true
	}
	if info, err := os.Stat(path); err == nil && within(info.ModTime()) {
		return true
	}
	return false
}
