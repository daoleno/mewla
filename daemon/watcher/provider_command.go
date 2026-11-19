package watcher

import (
	"errors"
	"fmt"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A provider command is one of the agent's own slash commands, typed into its
// Session outside any Turn. It is not a prompt: no Turn is admitted, no Work
// has to own the Session, and the Worker's turn contract is not appended (a
// wrapped command is no longer a command). Only commands that keep the
// conversation are allowed: compaction, the agents' own remedy for a
// conversation too large to send ("Request too large … Run /compact").

// ErrProviderCommandRefused: the command is not one Mewla types for this
// agent, or the agent cannot take it now.
var ErrProviderCommandRefused = errors.New("provider command refused")

// CompactCommand is the compaction command of the agents that have one.
const CompactCommand = "/compact"

// ProviderCompactCommand is the agent's compaction command for an executor
// command line, "" when the agent has none Mewla can type.
func ProviderCompactCommand(command string) string {
	switch {
	case isClaudeCommand(command), isCodexCommand(command), isPiCommand(command):
		return CompactCommand
	}
	return ""
}

// ValidateProviderCommand checks text is a provider command Mewla may type for
// the agent run by command: the agent's compaction command, optionally with
// instructions on the same line ("/compact keep the open questions").
func ValidateProviderCommand(command, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return "", fmt.Errorf("%w: a provider command is one line", ErrProviderCommandRefused)
	}
	compact := ProviderCompactCommand(command)
	if compact == "" {
		return "", fmt.Errorf("%w: this agent has no command Mewla can send", ErrProviderCommandRefused)
	}
	name, _, _ := strings.Cut(text, " ")
	if name != compact {
		return "", fmt.Errorf("%w: only %s can be sent to this agent", ErrProviderCommandRefused, compact)
	}
	return text, nil
}

// SendProviderCommand types a provider command into a Session whose agent is
// not working and submits it. An agent at work is refused rather than given a
// command it would queue as a message.
func (w *Watcher) SendProviderCommand(sessionID, text string) error {
	identity, known := w.targetForSession(sessionID)
	if !known {
		return definitelyNotSubmitted("", fmt.Errorf("target provider could not be proven"))
	}
	text, err := ValidateProviderCommand(identity.Command, text)
	if err != nil {
		return definitelyNotSubmitted("", err)
	}
	resolver := w.targetForSession
	return w.sessionInputOwner().serialized(sessionID, func() error {
		guard := func() error { return guardTargetIdentity(resolver, sessionID, identity) }
		if err := guard(); err != nil {
			return definitelyNotSubmitted("", err)
		}
		content, err := w.CapturePaneContent(sessionID)
		if err != nil {
			return definitelyNotSubmitted("", fmt.Errorf("read the Session's screen: %w", err))
		}
		// Only an agent visibly at work is refused. Its composer is not read:
		// after an error Claude leaves a dim suggestion there ("Run /rewind to
		// go back") that a screen capture shows as typed text, and typing
		// replaces it.
		if classifier.AgentWorkingOnScreen(content) {
			return definitelyNotSubmitted("", fmt.Errorf("%w: the agent is working; send it when its turn has ended", ErrProviderCommandRefused))
		}
		return w.sendDraftInputLocked(sessionID, text+"\n", submitDelay(identity.Command), guard)
	})
}
