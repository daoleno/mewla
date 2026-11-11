package classifier

import (
	"os"
	"testing"
)

// Claude Code 2.1.322 asks for folder trust with a menu that has no numbers and
// opens on "No, exit" (captured with `tmux capture-pane -p` at 50x40, the path
// replaced by /home/u/proj). Unread, the chat said "Ready" and a send's Enter
// answered "No, exit": the Session ended (FAILURES Round 3 #2).
func TestClaudeUnnumberedFolderTrustIsAStartupPrompt(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-2.1.322/folder-trust-50x40.txt")
	if err != nil {
		t.Fatal(err)
	}
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: string(raw)})
	if signal.State != StateBlocked || signal.Prompt == nil {
		t.Fatalf("the trust menu is not a prompt: %+v", signal)
	}
	prompt := signal.Prompt
	if prompt.Kind != PromptStartup || !prompt.Unnumbered || prompt.ID == "" {
		t.Fatalf("prompt %+v", prompt)
	}
	want := []PromptOption{{Number: 1, Label: "No, exit", Selected: true}, {Number: 2, Label: "Yes, I trust this folder"}}
	if len(prompt.Options) != len(want) {
		t.Fatalf("options %+v", prompt.Options)
	}
	for index, option := range prompt.Options {
		if option != want[index] {
			t.Fatalf("option %d = %+v, want %+v", index, option, want[index])
		}
	}
	// Moving the cursor is not a new prompt: an answer composed before it still applies.
	moved := replaceOnce(replaceOnce(string(raw), " ❯ No, exit", "   No, exit"), "   Yes, I trust this folder", " ❯ Yes, I trust this folder")
	again := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: moved})
	if again.Prompt == nil || again.Prompt.ID != prompt.ID || !again.Prompt.Options[1].Selected {
		t.Fatalf("after the cursor moved: %+v", again.Prompt)
	}
}

// Prose with a pointer above an idle composer is not a menu.
func TestClaudeCursorLineWithoutAMenuFooterIsNotAPrompt(t *testing.T) {
	pane := "Claude Code\n● Options:\n ❯ one\n   two\n\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	if signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: pane}); signal.State == StateBlocked {
		t.Fatalf("became a prompt: %+v", signal)
	}
}
