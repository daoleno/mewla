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

func tmuxPaneShowing(t *testing.T, historyLimit string, rows string, body string) (socket, target string) {
	t.Helper()
	requireTmux(t)
	dir := shortTmuxTestDir(t)
	socket = filepath.Join(dir, "s")
	script := filepath.Join(dir, "show.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\ncat <<'MEWLA_FRAME'\n"+body+"MEWLA_FRAME\nsleep 60\n"), 0o755); err != nil {
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

// Trailing spaces inside a multi-line quoted string survive the real capture
// and so tell two commands apart.
func TestRealTmuxTrailingSpacesInAQuotedCommandAreIdentity(t *testing.T) {
	ids := map[string]string{}
	for name, command := range map[string]string{"one": "printf 'alpha \nbeta'", "two": "printf 'alpha  \nbeta'"} {
		frame := "OpenAI Codex\n  Would you like to run the following command?\n\n  $ " + command + "\n\n› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
		socket, target := tmuxPaneShowing(t, "2000", "24", frame)
		signal, _ := codexSignalFrom(t, socket, target)
		if signal.Prompt == nil {
			t.Fatalf("%s: no prompt: %+v", name, signal)
		}
		ids[name] = signal.Prompt.ID
	}
	if ids["one"] == ids["two"] {
		t.Fatalf("one and two trailing spaces share id %s", ids["one"])
	}
}
