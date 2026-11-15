package classifier

import (
	"regexp"
	"strings"
)

// AgentWorkingOnScreen reports whether an agent's own screen shows it working
// right now: its status footer carries the interrupt hint (Claude Code and
// Codex show "esc to interrupt" only while a turn runs), or Claude's spinner
// line sits just above its prompt with the time the turn has run, such as
// "✻ Calculating… (1h 34m 36s · thinking with high effort)". Only the bottom
// of the screen counts: the same words in the conversation above prove
// nothing. A finished turn's line ("✻ Cogitated for 6m 48s · done 17:49") is
// not working.
func AgentWorkingOnScreen(pane string) bool {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	seen := 0
	for index := len(lines) - 1; index >= 0 && seen < agentScreenBottomLines; index-- {
		line := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(lines[index]), "│┃|"))
		if line == "" {
			continue
		}
		seen++
		if seen <= agentFooterLines && agentInterruptRe.MatchString(line) {
			return true
		}
		if agentSpinnerRe.MatchString(line) {
			return true
		}
	}
	return false
}

// The status footer is the last lines of the screen (a narrow one wraps it);
// the spinner line is above the prompt box and a tip under it.
const (
	agentFooterLines       = 3
	agentScreenBottomLines = 10
)

var (
	agentInterruptRe = regexp.MustCompile(`(?i)\besc to interrupt\b`)
	// A glyph, a verb with an ellipsis, then the running time: "(12s",
	// "(8m 48s", "(59m", "(2h 39m 51s".
	agentSpinnerRe = regexp.MustCompile(`^[^\p{L}\p{N}\s]\s+\p{L}[\p{L}'’-]*…\s+\(\d+[hms]\b`)
)
