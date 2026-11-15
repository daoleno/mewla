package watcher

import (
	"os"
	"testing"
	"time"
)

// A Claude Code that never ran shows its theme picker, then a security note;
// waiting for input answers both with Enter (they decide nothing) instead of
// leaving a Brain or a Worker there until someone opens its terminal.
func TestClaudeFirstRunScreensAreAnsweredButNothingElse(t *testing.T) {
	theme, err := os.ReadFile("../classifier/testdata/claude-2.1.325/first-run-theme-windows.txt")
	if err != nil {
		t.Fatal(err)
	}
	security := "Welcome to Claude Code v2.1.325\n\n Security notes:\n\n 1. Claude can make mistakes.\n    You're responsible for Claude's actions and should always\n    review them, especially when running code.\n\n 2. Due to prompt injection risks, only use it with code you trust\n    Learn more: https://code.claude.com/docs/en/security\n\n Press Enter to continue…\n"
	trust := " Accessing workspace:\n\n C:\\Users\\me\\proj1\n\n Quick safety check: Is this a project you created or one you trust?\n\n > No, exit\n   Yes, I trust this folder\n\n Enter to confirm · Esc to cancel\n"
	login := " Select login method:\n\n > 1. Claude account with subscription\n   2. Anthropic Console account\n\n"
	for name, c := range map[string]struct {
		command, screen string
		want            string
	}{
		"theme":                   {"claude --session-id x", string(theme), "theme"},
		"security note":           {"claude", security, "security"},
		"folder trust (No, exit)": {"claude", trust, ""},
		"login choice":            {"claude", login, ""},
		"theme on another agent":  {"codex", string(theme), ""},
		"note quoted in a chat":   {"claude", security + "\n> what does that note mean?\n", ""},
	} {
		if got := claudeFirstRunScreen(c.command, c.screen); got != c.want {
			t.Errorf("%s: screen=%q, want %q", name, got, c.want)
		}
	}

	// Each screen once; the same one again only if it is still there a while
	// later (the theme picker stays drawn for a moment after its Enter, and a
	// second Enter would land on the security note's successor).
	answered, now := claudeFirstRun{}, time.Now()
	if !answered.due("theme", now) {
		t.Fatal("first theme not due")
	}
	answered["theme"] = now
	if answered.due("theme", now.Add(time.Second)) || !answered.due("security", now.Add(time.Second)) || !answered.due("theme", now.Add(claudeFirstRunRepeat)) {
		t.Fatal("first-run screens answered more than once, or not at all")
	}
}
