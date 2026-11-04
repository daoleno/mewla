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
	pane := latestClaudePaneWindow(in.PaneContent)
	whole := strings.ReplaceAll(in.PaneContent, "\r\n", "\n")
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
		signal := ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for plan approval",
			Source:   "claude_pane_plan",
			Provider: a.Name(),
		}
		if prompt, ok := ParseMenuPromptIn(a.Name(), PromptPlan, pane, whole); ok {
			prompt.Title = signal.Summary
			signal.Prompt = &prompt
		}
		return signal
	}
	if permission := claudePromptTail(pane, 12); claudeTailHasPrefix(permission, "Do you want to") &&
		len(permission) > 0 && strings.HasPrefix(permission[0], "Esc to cancel") {
		signal := ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Claude permission",
			Source:   "claude_pane_blocked",
			Provider: a.Name(),
		}
		if prompt, ok := ParseMenuPromptIn(a.Name(), PromptPermission, pane, whole, "Esc to cancel"); ok {
			if prompt.Title == "" {
				prompt.Title = signal.Summary
			}
			signal.Prompt = &prompt
		}
		return signal
	}
	// Launch-time menus (trust this folder, accept a mode, pick a theme) end
	// in "Enter to confirm" and appear before any conversation exists.
	if prompt, ok := ParseMenuPromptIn(a.Name(), PromptStartup, pane, whole, "Enter to confirm"); ok {
		prompt.Title = "Waiting for your choice"
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  prompt.Title,
			Source:   "claude_pane_startup",
			Provider: a.Name(),
			Prompt:   &prompt,
		}
	}

	return ActivitySignal{State: StateUnknown, Source: "claude_idle", Provider: a.Name()}
}

// claudeBannerRe is Claude's own banner line: its name alone, or with a
// version, after any logo glyphs. A mention inside a command, a commit message
// or the auto-refusal notice ("Claude Code will automatically ...") is not
// one, and taking it for one would start the pane window inside the open
// dialog and cut the command above it away.
var claudeBannerRe = regexp.MustCompile(`(?im)^[^\p{L}\p{N}\n]*claude code\b(?:[ \t]+v?\d[^\n]*)?[ \t\r]*$`)

// latestClaudePaneWindow is the pane from Claude's latest banner on, or its
// last 80 lines when the banner has scrolled out.
func latestClaudePaneWindow(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if matches := claudeBannerRe.FindAllStringIndex(normalized, -1); len(matches) > 0 {
		start := matches[len(matches)-1][0]
		// The match opens at the line start; keep the banner line itself.
		return normalized[start:]
	}
	return latestProviderPaneWindow(normalized, "")
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
