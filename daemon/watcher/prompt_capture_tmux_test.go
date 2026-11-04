package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// These tests run the real capture (capturePaneDialog) against a real tmux
// server on a private socket, so what they assert is what tmux actually keeps:
// trailing spaces a program wrote, joined wrapped lines, and whether the
// capture reaches the pane's first line.

func tmuxPaneShowing(t *testing.T, historyLimit string, rows string, frames ...string) (socket, target string) {
	t.Helper()
	requireTmux(t)
	dir := shortTmuxTestDir(t)
	socket = filepath.Join(dir, "s")
	script := filepath.Join(dir, "show.sh")
	program := "#!/bin/bash\n"
	for index, frame := range frames {
		program += "cat <<'MEWLA_FRAME'\n" + frame + "MEWLA_FRAME\n"
		if index < len(frames)-1 {
			program += "sleep 0.6\n"
		}
	}
	if err := os.WriteFile(script, []byte(program+"sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Raw fixture bootstrap, as the other real-tmux tests do: an empty config
	// and a scrollback limit chosen for the case.
	if out, err := exec.Command("tmux", "-S", socket, "-f", "/dev/null", "start-server", ";", "set-option", "-g", "history-limit", historyLimit, ";",
		"new-session", "-d", "-s", "t", "-x", "60", "-y", rows, script).CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		text, _, err := capturePaneDialog(socket, "t")
		if err == nil && strings.Contains(text, "MEWLA-END") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return socket, "t"
}

func TestCapturePaneDialogKeepsWrittenTrailingSpacesAndJoinsWrappedLines(t *testing.T) {
	long := strings.Repeat("0123456789", 7) // wider than the 60-column pane: tmux wraps it
	socket, target := tmuxPaneShowing(t, "2000", "24", "alpha \nbeta\nx  \ny\n"+long+"\nMEWLA-END\n")
	text, info, err := capturePaneDialog(socket, target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exact || !info.Complete {
		t.Fatalf("capture info = %+v, want exact and complete (scrollback is far below its limit)", info)
	}
	for _, want := range []string{"alpha \n", "x  \n", long + "\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the capture lost %q:\n%q", want, text)
		}
	}
}

func dialogFrame(operation string) string {
	return "OpenAI Codex\n• Ran ls\n\n  Would you like to run the following command?\n\n  $ " + operation +
		"\ncat <<'EOF'\nWould you like to run the following command?\n" + strings.Repeat("common\n", 19) + "EOF\n\n" +
		"› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
}

func codexSignalFrom(t *testing.T, socket, target string) (classifier.ActivitySignal, classifier.CaptureInfo) {
	t.Helper()
	text, info, err := capturePaneDialog(socket, target)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "MEWLA-END\n", "", 1)
	return classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "codex"}, Capture: &info, PaneContent: text}), info
}

// The scrollback is cut inside the command, at its lookalike heading: the
// capture says it is not complete, and no id is issued, for either operation.
func TestRealTmuxClippedAtALookalikeHeadingGivesNoID(t *testing.T) {
	// 24 rows, 4 lines of scrollback: of the 39-line frame only the last 28 survive,
	// starting at the lookalike heading inside the command.
	for _, operation := range []string{"echo ALPHA", "echo BETA"} {
		socket, target := tmuxPaneShowing(t, "4", "24", dialogFrame(operation))
		text, _, err := capturePaneDialog(socket, target)
		if err != nil {
			t.Fatal(err)
		}
		if first := strings.TrimSpace(strings.SplitN(strings.TrimLeft(text, "\n"), "\n", 2)[0]); first != "Would you like to run the following command?" || strings.Contains(text, operation) {
			t.Fatalf("%s: fixture is not clipped at the payload heading (first line %q, operation still present: %v)", operation, first, strings.Contains(text, operation))
		}
		signal, info := codexSignalFrom(t, socket, target)
		if info.Complete {
			t.Fatalf("%s: a pane whose scrollback was evicted reported a complete capture", operation)
		}
		if signal.State != classifier.StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: clipped capture produced %+v", operation, signal)
		}
	}
}

// The same dialog with its scrollback intact is complete, answerable, and the
// two operations get different ids.
func TestRealTmuxCompleteCaptureIdentifiesTheOperation(t *testing.T) {
	ids := map[string]string{}
	for _, operation := range []string{"echo ALPHA", "echo BETA"} {
		socket, target := tmuxPaneShowing(t, "2000", "24", dialogFrame(operation))
		signal, info := codexSignalFrom(t, socket, target)
		if !info.Complete || signal.Prompt == nil || !strings.Contains(signal.Prompt.Request, operation) {
			t.Fatalf("%s: complete capture: %+v / %+v", operation, info, signal)
		}
		ids[operation] = signal.Prompt.ID
	}
	if ids["echo ALPHA"] == ids["echo BETA"] {
		t.Fatalf("different operations share id %s", ids["echo ALPHA"])
	}
}

// What the real capture reports for blanks at a line's end: a space a program
// wrote is kept, and so is the stale extent of a longer line that was
// overwritten in place and erased. The two cannot be told apart, so a dialog
// with such a line followed by more request text is refused, and one drawn
// with whole-line erases (what a redrawing TUI does) is identified.
func TestRealTmuxBlanksAtALineEndAreNotEvidence(t *testing.T) {
	// 1. A command whose quoted string has a written space before its newline.
	written := "OpenAI Codex\n  Would you like to run the following command?\n\n  $ printf 'alpha \nbeta'\n\n› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
	socket, target := tmuxPaneShowing(t, "2000", "24", written)
	text, _, err := capturePaneDialog(socket, target)
	if err != nil || !strings.Contains(text, "printf 'alpha \n") {
		t.Fatalf("tmux did not keep the written space (err %v):\n%q", err, text)
	}
	if signal, _ := codexSignalFrom(t, socket, target); signal.State != classifier.StateBlocked || signal.Prompt != nil {
		t.Fatalf("a quoted trailing space was identified: %+v", signal)
	}

	// 2. The same ordinary dialog drawn over longer lines. Erase-to-end-of-line
	// leaves the old extent reported as trailing spaces; a whole-line erase
	// first does not.
	long := strings.Repeat("x", 50)
	older := "OpenAI Codex\n" + strings.Repeat(long+"\n", 8)
	draw := func(erase string) string {
		lines := []string{"OpenAI Codex", "  Would you like to run the following command?", "", "  $ echo hi", "", "› 1. Yes, proceed (y)", "  2. No, cancel", "", "  Press enter to confirm or esc to cancel"}
		out := "\x1b[H"
		for _, line := range lines {
			if erase == "line" {
				out += "\x1b[2K" + line + "\n"
			} else {
				out += line + "\x1b[K\n"
			}
		}
		return out + "MEWLA-END\n"
	}
	stale, staleTarget := tmuxPaneShowing(t, "2000", "24", older, draw("eol"))
	staleText, _, _ := capturePaneDialog(stale, staleTarget)
	if !strings.Contains(staleText, "  $ echo hi ") {
		t.Fatalf("the fixture left no stale blanks after the command line:\n%q", staleText)
	}
	if signal, _ := codexSignalFrom(t, stale, staleTarget); signal.State != classifier.StateBlocked || signal.Prompt != nil {
		t.Fatalf("stale padding was taken for the command's text: %+v", signal)
	}
	fresh, freshTarget := tmuxPaneShowing(t, "2000", "24", older, draw("line"))
	if signal, _ := codexSignalFrom(t, fresh, freshTarget); signal.Prompt == nil {
		t.Fatalf("a dialog drawn with whole-line erases was refused: %+v", signal)
	}
}
