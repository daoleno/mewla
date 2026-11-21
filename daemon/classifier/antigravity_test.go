package classifier

import (
	"strings"
	"testing"
)

// The screens below are agy 1.5.14 in a tmux pane (captured 2026-11-22).
const antigravityRule = "────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────"

func antigravityScreen(lines ...string) []string {
	return strings.Split(strings.Join(lines, "\n"), "\n")
}

func TestClassifyAntigravityModelErrorIsFailedAndNamesTheError(t *testing.T) {
	screen := antigravityScreen(
		"> Create a file named hello.txt",
		"⚠ agent executor error: generating and executing: Error 400, Message: API key not valid. Please pass a valid API key.,",
		"Error ID: 6938ea36-55eb-41ed-be36-89bab841c793-1",
		antigravityRule, ">", antigravityRule,
		"? for shortcuts                                                              Gemini 3.1 Pro · low",
	)
	state, summary := Classify(true, screen, "agy")
	if state != StateFailed || !strings.Contains(summary, "agent executor error") {
		t.Fatalf("state=%s summary=%q", state, summary)
	}
}

func TestClassifyAntigravityWaitingSummaryIsNotItsComposerFrame(t *testing.T) {
	screen := antigravityScreen(
		"Done: wrote hello.txt",
		antigravityRule, ">", antigravityRule,
		"? for shortcuts                                                              Gemini 3.1 Pro · low",
	)
	state, summary := Classify(true, screen, "agy --dangerously-skip-permissions")
	if state != StateUnknown || summary != "Done: wrote hello.txt" {
		t.Fatalf("state=%s summary=%q", state, summary)
	}
}

func TestClassifyAntigravitySignInAndTrustWaitForAPerson(t *testing.T) {
	signIn := antigravityScreen(
		"Welcome to the Antigravity CLI. You are currently not signed in.",
		"Select login method:",
		"> 1. Google OAuth",
		"  2. Use a Google Cloud project",
		"↑/↓ Navigate · enter Select",
	)
	if state, _ := Classify(true, signIn, "agy"); state != StateBlocked {
		t.Fatalf("sign-in menu state = %s", state)
	}
	trust := antigravityScreen(
		"Accessing workspace:",
		"/home/u/project",
		"Do you trust the contents of this project?",
		"Antigravity CLI requires permission to read, edit, and execute files here.",
		"> Yes, I trust this folder",
		"  No, exit",
		"↑/↓ Navigate · enter Confirm",
	)
	if state, _ := Classify(true, trust, "agy"); state != StateBlocked {
		t.Fatalf("trust dialog state = %s", state)
	}
	// Another agent's pane that quotes the same words is not agy waiting.
	if state, _ := Classify(true, signIn[:2], "claude"); state == StateFailed {
		t.Fatalf("quoted text must not fail another agent")
	}
}

// agy 1.5.14, toolPermission "strict", captured 2026-11-22: the permission dialog.
func TestClassifyAntigravityPermissionDialogIsBlocked(t *testing.T) {
	screen := antigravityScreen(
		"> Run the shell command: echo approval-test",
		"● Ran (echo approval-test) (ctrl+o to expand)",
		"Command",
		antigravityRule,
		"Requesting permission for:",
		"   echo approval-test",
		"Run this command?",
		"> 1. Yes, run command",
		"  2. No, cancel",
		"  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command",
		"esc to cancel                                                       Gemini 4 Neon · low",
	)
	if state, _ := Classify(true, screen, "agy"); state != StateBlocked {
		t.Fatalf("permission dialog state = %s", state)
	}
}

// agy 1.5.14 on a fresh Windows install, captured 2026-11-22: after a sign-in it
// asks for a colour scheme, then the terms. The scheme preview's last line is
// "press Enter to continue", which used to become the Worker's summary.
func TestClassifyAntigravityFirstRunChoicesAreBlockedAndSaySo(t *testing.T) {
	theme := antigravityScreen(
		"Welcome to Antigravity CLI!",
		"",
		"Choose your color scheme:        ╭──────────────────────────────╮",
		"  > terminal                     │ > you: add a greeting function │",
		"    light                        │   · dim: press Enter to continue │",
		"                                 ╰──────────────────────────────╯",
		"    [Next]",
		"  ↑/↓ Navigate · enter Confirm",
	)
	terms := antigravityScreen(
		"Terms of Service & Data Use",
		"  > [x] Yes, I agree to help improve Antigravity CLI",
		"[Previous]      [Done]",
		"  ↑/↓ Navigate · enter Toggle",
	)
	for name, screen := range map[string][]string{"theme": theme, "terms": terms} {
		state, summary := Classify(true, screen, "agy --dangerously-skip-permissions")
		if state != StateBlocked || summary != "Finish Antigravity's first-run setup in the terminal" {
			t.Fatalf("%s: state=%s summary=%q", name, state, summary)
		}
	}
	// Another agent's pane quoting the same words is not agy's first run.
	if _, summary := Classify(true, theme, "claude"); summary == "Finish Antigravity's first-run setup in the terminal" {
		t.Fatalf("another agent's pane was classified as agy's first run")
	}
}
