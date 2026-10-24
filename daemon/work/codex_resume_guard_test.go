package work

import "testing"

func TestIsCodexResumeCommandToleratesEmptyCommands(t *testing.T) {
	for _, command := range []string{"", "   ", "codex", "codex exec x"} {
		if isCodexResumeCommand(command) {
			t.Errorf("%q is not a resume", command)
		}
	}
	if !isCodexResumeCommand("codex resume abc") {
		t.Fatal("resume not recognised")
	}
}
