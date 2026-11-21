package watcher

import (
	"regexp"
	"strings"
)

// agy (Google's Antigravity CLI) screens, taken from agy 1.5.14 in a tmux pane:
//
//	header:      "Antigravity CLI 1.5.14", the account or key line, the model
//	composer:    a rule, a "> " line, a rule, then "? for shortcuts" and the model
//	not signed in: "Select login method:" with "1. Google OAuth"
//	trust:       "Do you trust the contents of this project?"
//
// Only the screens that stop a launch are named. They are the person's to
// answer (sign-in, and the first-run choices that follow it), never Mewla's.
var (
	antigravityHeaderRe = regexp.MustCompile(`Antigravity CLI \d+\.\d+`)
	antigravityRuleRe   = regexp.MustCompile(`(?m)^\s*─{20,}\s*$`)
	// antigravityPlaceholderRe is the dim hint agy 1.5.14 shows in an empty
	// composer in Auto mode: "Auto mode: edits & safe commands auto-approved
	// (shift+tab to cycle)". It is not a draft.
	antigravityPlaceholderRe = regexp.MustCompile(`^>\s+.*\(shift\+tab to cycle\)$`)
	antigravityFooterRe      = regexp.MustCompile(`(?m)^\? for shortcuts\b`)
)

// latestAntigravityContent is the screen from agy's last start: scrollback
// before a restart in the same pane is not the current screen.
func latestAntigravityContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > 120 {
		content = strings.Join(lines[len(lines)-120:], "\n")
	}
	if headers := antigravityHeaderRe.FindAllStringIndex(content, -1); len(headers) > 0 {
		return content[headers[len(headers)-1][0]:]
	}
	return content
}

// isAntigravityInputReady reports an agy that is signed in, past its first-run
// and trust screens, and waiting at an empty composer.
func isAntigravityInputReady(content string) bool {
	if !antigravityHeaderRe.MatchString(content) {
		return false
	}
	current := latestAntigravityContent(content)
	for _, blocked := range []string{
		"Do you trust the contents of this project",
		"Select login method",
		"You are currently not signed in",
		"Terms of Service & Data Use",
		"Choose your color scheme",
		"Settings Error",
	} {
		if strings.Contains(current, blocked) {
			return false
		}
	}
	rules := antigravityRuleRe.FindAllStringIndex(current, -1)
	if len(rules) < 2 || !antigravityFooterRe.MatchString(current) {
		return false
	}
	// An empty composer: the "> " line between the last two rules is bare.
	between := current[rules[len(rules)-2][1]:rules[len(rules)-1][0]]
	text := strings.TrimSpace(between)
	return text == ">" || antigravityPlaceholderRe.MatchString(text)
}
