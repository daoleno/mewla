package classifier

import (
	"strings"
	"testing"
)

// narrowClaudePermission is a Claude permission box drawn width columns wide
// (the rule above it spans the pane), its tip wrapped as Claude wraps it:
// word by word, up to that width.
func narrowClaudePermission(width int, tip []string, description string) string {
	rule := strings.Repeat("─", width)
	dashes := strings.Repeat("╌", width)
	lines := []string{"● I'll fetch the headers.", "", rule, " Bash command"}
	for _, line := range tip {
		lines = append(lines, " "+line)
	}
	lines = append(lines, " "+description, dashes, " curl -sI https://example.edu", dashes,
		" Do you want to proceed?", " ❯ 1. Yes", "   2. Yes, and don't ask again", "   3. No", "", " Esc to cancel · Tab to amend")
	return strings.Join(lines, "\n")
}

// On a narrow pane (the phone's terminal at 72 columns) Claude's tip takes two
// rows. The whole tip is the provider's advice, its wrapped tail too: the
// request starts at what is asked, never at 'mode" below'.
func TestAClaudeTipWrappedOnANarrowPaneLeavesNoTailInTheRequest(t *testing.T) {
	pane := narrowClaudePermission(72, []string{
		`Tip: auto mode handles these prompts for you — choose "switch to auto`,
		`mode" below`,
	}, "Fetch HTTP headers from example.edu")
	prompt, _, ok := ParseClaudePermissionPrompt(pane)
	if !ok {
		t.Fatal("no prompt")
	}
	if !strings.HasPrefix(prompt.Request, "Fetch HTTP headers from example.edu") || strings.Contains(prompt.Request, "below") {
		t.Fatalf("request = %q", prompt.Request)
	}
	// Three rows on a narrower pane.
	pane = narrowClaudePermission(30, []string{"Tip: auto mode handles these", `prompts for you — choose`, `"switch to auto mode" below`}, "Fetch headers")
	if prompt, _, _ = ParseClaudePermissionPrompt(pane); !strings.HasPrefix(prompt.Request, "Fetch headers") {
		t.Fatalf("request at 30 columns = %q", prompt.Request)
	}
}

// A tip that fits its row ends there, and of a tip Mewla does not know only its
// own row goes: the description under either is what is asked, whatever the
// width.
func TestATipKeepsTheDescriptionUnderIt(t *testing.T) {
	pane := narrowClaudePermission(120, []string{`Tip: auto mode handles these prompts for you — choose "switch to auto mode" below`}, "Fetch")
	if prompt, _, ok := ParseClaudePermissionPrompt(pane); !ok || !strings.HasPrefix(prompt.Request, "Fetch\n") {
		t.Fatalf("request = %q (ok %v)", prompt.Request, ok)
	}
	pane = narrowClaudePermission(30, []string{"Tip: a newer hint, wrapped", "onto a second row"}, "Fetch headers")
	if prompt, _, _ := ParseClaudePermissionPrompt(pane); !strings.HasPrefix(prompt.Request, "onto a second row\nFetch headers") {
		t.Fatalf("an unknown tip took more than its row: %q", prompt.Request)
	}
}
