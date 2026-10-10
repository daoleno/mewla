package classifier

import (
	"regexp"
	"strings"
)

var claudeChromeRe = regexp.MustCompile(`(?i)\bclaude\s+code\b`)

// ClaudeActivityAdapter recognizes only visible permission/approval evidence.
// Every Claude prompt is proven by its footer at the bottom of the pane; words
// in the conversation above ("permission", "AskUserQuestion") prove nothing.
// Canonical provider transcripts remain owned by daemon/work.
type ClaudeActivityAdapter struct{}

func NewClaudeActivityAdapter() *ClaudeActivityAdapter {
	return &ClaudeActivityAdapter{}
}

func (a *ClaudeActivityAdapter) Name() string { return "claude" }

func (a *ClaudeActivityAdapter) Match(in ActivityInput) bool {
	base := commandBaseName(in.Worker.Command)
	if base == "claude" || base == "cc" || strings.Contains(base, "claude") {
		return true
	}
	return claudeChromeRe.MatchString(in.PaneContent)
}

func (a *ClaudeActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	pane := latestProviderPaneWindow(in.PaneContent, "claude code")
	// Claude's selection prompts sit at the bottom of the pane; only the
	// footer region proves one is open now.
	tail := claudePromptTail(pane, 8)
	switch {
	case claudeTailHasPrefix(tail, "Enter to select") || claudeTailHasPrefix(tail, "Ready to submit your answers?"):
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for your choice",
			Source:   "claude_pane_choice",
			Provider: a.Name(),
		}
	case claudeTailHasPrefix(tail, "Claude has written up a plan"):
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for plan approval",
			Source:   "claude_pane_plan",
			Provider: a.Name(),
		}
	}
	if permission := claudePromptTail(pane, 12); claudeTailHasPrefix(permission, "Do you want to") &&
		len(permission) > 0 && strings.HasPrefix(permission[0], "Esc to cancel") {
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Claude permission",
			Source:   "claude_pane_blocked",
			Provider: a.Name(),
		}
	}

	return ActivitySignal{State: StateUnknown, Source: "claude_idle", Provider: a.Name()}
}

// claudePromptTail returns the last n non-blank lines, trimmed.
func claudePromptTail(pane string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	var tail []string
	for index := len(lines) - 1; index >= 0 && len(tail) < n; index-- {
		if trimmed := strings.TrimSpace(lines[index]); trimmed != "" {
			tail = append(tail, trimmed)
		}
	}
	return tail
}

func claudeTailHasPrefix(tail []string, prefix string) bool {
	for _, line := range tail {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
