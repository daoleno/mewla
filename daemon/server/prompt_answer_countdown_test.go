package server

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// %729 (Work 72aa5b49), 2026-11-05: Brain was told of a Claude permission while
// its "will automatically block this request in m:ss" clock read 2:00, and its
// answer at 1:25 was refused as "passed" although the same dialog stood open.
func TestAnswerPanePromptSurvivesBlockCountdownAndStillRefusesOtherCommands(t *testing.T) {
	const notice = "⏱ Claude Code will automatically block this request in %s, to avoid holding up an unattended session"
	pane := func(command, left string) string {
		return "Claude Code v2.1.0\n● writing the prototype\n────────────────\n Bash command\n\n   " + command + "\n   Write prototype page\n\n" +
			" Dangerous rm operation not ruled out\n\n " + strings.Replace(notice, "%s", left, 1) + "\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	}
	live := func(command, left string) *classifier.Prompt {
		t.Helper()
		signal := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "claude"}, Capture: &classifier.CaptureInfo{Exact: true, Complete: true}, PaneContent: pane(command, left)})
		if signal.Prompt == nil {
			t.Fatalf("no prompt at %s: %+v", left, signal)
		}
		return signal.Prompt
	}
	loads := func() (work.CodexConversation, error) { return work.CodexConversation{}, nil }
	const command = "cat > /tmp/proto/index.html <<'EOF'"
	told := live(command, "2:00")

	same := &fakePanePrompts{current: live(command, "1:25")}
	if _, err := AnswerPanePrompt(same, "%729", PromptAnswer{PromptID: told.ID, Number: 1}, nil, loads); err != nil {
		t.Fatalf("the same dialog, 35 seconds on, was refused: %v", err)
	}
	if len(same.pressed) != 1 || same.pressed[0] != 1 {
		t.Fatalf("pressed %v, want [1]", same.pressed)
	}

	other := &fakePanePrompts{current: live("rm -rf /tmp/proto", "0:49")}
	if _, err := AnswerPanePrompt(other, "%729", PromptAnswer{PromptID: told.ID, Number: 1}, nil, loads); !PromptGone(err) {
		t.Fatalf("a different command was answered with the old prompt id: %v", err)
	}
	if len(other.pressed) != 0 {
		t.Fatalf("keys sent to a different dialog: %v", other.pressed)
	}

	expired := &fakePanePrompts{}
	if _, err := AnswerPanePrompt(expired, "%729", PromptAnswer{PromptID: told.ID, Number: 1}, nil, loads); !PromptGone(err) || len(expired.pressed) != 0 {
		t.Fatalf("an expired dialog was answered: %v %v", err, expired.pressed)
	}
}
