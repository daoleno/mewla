package classifier

import (
	"os"
	"testing"
)

// Claude Code 2.1.332 asks "Switch model?" when /model changes the model of a
// conversation it has cached (frame from the 0.4.29 gate's pane poll, clock
// changed). Mewla's Model sheet answers it itself, about 0.3 s after it shows.
// Its footer ("Enter to confirm") is the launch-time menus' footer, so a screen
// read that caught it made it a startup prompt, "Waiting for your choice"; once
// it left, the chat sealed it as "You chose · Waiting for your choice" (gate c,
// held pick).
func TestClaudeModelSwitchConfirmIsNotAPrompt(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-2.1.332/model-switch-confirm.txt")
	if err != nil {
		t.Fatal(err)
	}
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: string(raw)})
	if signal.State == StateBlocked || signal.Prompt != nil {
		t.Fatalf("Claude's model switch confirm became a prompt: %+v (prompt %+v)", signal, signal.Prompt)
	}
	if !ClaudeModelSwitchConfirm(string(raw)) {
		t.Fatal("the confirm is not recognised")
	}
}

// A launch-time menu with the same footer stays a prompt.
func TestClaudeModelSwitchConfirmLeavesOtherConfirmMenus(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-2.1.322/folder-trust-50x40.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ClaudeModelSwitchConfirm(string(raw)) {
		t.Fatal("the folder trust menu read as the model switch confirm")
	}
	// The words in the conversation above an idle prompt prove nothing.
	pane := "● Claude asked: Switch model? 1. Yes, switch to Sonnet 5.5\n\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	if ClaudeModelSwitchConfirm(pane) {
		t.Fatal("conversation text read as the confirm")
	}
}
