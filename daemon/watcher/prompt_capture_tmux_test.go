package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// These tests run the real capture (capturePaneDialog) against a real tmux
// server on a private socket, with panes launched the way Mewla launches a
// Worker (the origin marker printed first, its nonce in the pane's own
// option), so what they assert is what tmux actually keeps: trailing spaces a
// program wrote, joined wrapped lines, and whether the capture still starts at
// the pane's first output.

const testNonce = "00112233445566778899aabb"

// launchedPane starts a pane that prints the origin marker and then the frames
// (or, with no marker, just the frames, as an adopted pane would). The default
// is 60 columns wide.
func launchedPane(t *testing.T, historyLimit, rows string, marker bool, frames ...string) (socket, target string) {
	t.Helper()
	return launchedPaneSized(t, historyLimit, "60", rows, marker, frames...)
}

func launchedPaneSized(t *testing.T, historyLimit, cols, rows string, marker bool, frames ...string) (socket, target string) {
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
	// and a scrollback limit chosen for the case. The pane is allocated, its
	// origin option set, and then respawned with the marker command in front,
	// in the order Mewla's launch uses.
	if out, err := exec.Command("tmux", "-S", socket, "-f", "/dev/null", "start-server", ";", "set-option", "-g", "history-limit", historyLimit, ";",
		"new-session", "-d", "-s", "t", "-x", cols, "-y", rows, "sleep 86400").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	command := "exec " + script
	if marker {
		if out, err := exec.Command("tmux", "-S", socket, "set-option", "-p", "-t", "t", originOption, testNonce).CombinedOutput(); err != nil {
			t.Fatalf("tmux: %v: %s", err, out)
		}
		command = originMarkerCommand(testNonce) + command
	}
	if out, err := exec.Command("tmux", "-S", socket, "respawn-pane", "-k", "-t", "t", command).CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v: %s", err, out)
	}
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

// startsAtOrigin is what a capture needs to prove the dialog's first lines are
// in it: the pane's marker is its first line.
func startsAtOrigin(text string, info classifier.CaptureInfo) bool {
	first, _, _ := strings.Cut(text, "\n")
	return info.Origin != "" && strings.TrimRight(first, " ") == info.Origin
}

func TestCapturePaneDialogKeepsWrittenTrailingSpacesAndJoinsWrappedLines(t *testing.T) {
	long := strings.Repeat("0123456789", 7) // wider than the 60-column pane: tmux wraps it
	socket, target := launchedPane(t, "2000", "24", true, "alpha \nbeta\nx  \ny\n"+long+"\nMEWLA-END\n")
	text, info, err := capturePaneDialog(socket, target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exact || info.Origin != classifier.OriginMarkerLine(testNonce) || !startsAtOrigin(text, info) {
		t.Fatalf("capture info = %+v, first line %q: want exact with the pane's marker first", info, strings.SplitN(text, "\n", 2)[0])
	}
	for _, want := range []string{"alpha \n", "x  \n", long + "\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the capture lost %q:\n%q", want, text)
		}
	}
}

func dialogFrame(operation string) string {
	return "OpenAI Codex\n• Ran ls\n\n  Would you like to run the following command?\n\n  $ " + operation +
		"\n  cat <<'EOF'\n  Would you like to run the following command?\n" + strings.Repeat("  common\n", 19) + "  EOF\n\n" +
		"› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
}

func codexSignalFrom(t *testing.T, socket, target string) (classifier.ActivitySignal, string, classifier.CaptureInfo) {
	t.Helper()
	text, info, err := capturePaneDialog(socket, target)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "MEWLA-END\n", "", 1)
	return classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "codex"}, Capture: &info, PaneContent: text}), text, info
}

// The scrollback is cut inside the command, at its lookalike heading: the
// pane's marker is gone from the capture, and no id is issued, for either
// operation.
func TestRealTmuxClippedAtALookalikeHeadingGivesNoID(t *testing.T) {
	for _, operation := range []string{"echo ALPHA", "echo BETA"} {
		socket, target := launchedPane(t, "4", "24", true, dialogFrame(operation))
		text, info, err := capturePaneDialog(socket, target)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, operation) || !strings.Contains(text, "Would you like to run the following command?") || startsAtOrigin(text, info) {
			t.Fatalf("%s: fixture is not clipped inside the command:\n%s", operation, text)
		}
		signal, _, _ := codexSignalFrom(t, socket, target)
		if signal.State != classifier.StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: clipped capture produced %+v", operation, signal)
		}
	}
}

// The same dialog with its scrollback intact starts at the marker, is
// answerable, and the two operations get different ids.
func TestRealTmuxCompleteCaptureIdentifiesTheOperation(t *testing.T) {
	ids := map[string]string{}
	for _, operation := range []string{"echo ALPHA", "echo BETA"} {
		socket, target := launchedPane(t, "2000", "24", true, dialogFrame(operation))
		signal, text, info := codexSignalFrom(t, socket, target)
		if !startsAtOrigin(text, info) || signal.Prompt == nil || !strings.Contains(signal.Prompt.Request, operation) {
			t.Fatalf("%s: complete capture: %+v / %+v", operation, info, signal)
		}
		ids[operation] = signal.Prompt.ID
	}
	if ids["echo ALPHA"] == ids["echo BETA"] {
		t.Fatalf("different operations share id %s", ids["echo ALPHA"])
	}
}

// A pane Mewla did not launch has no origin: its dialog is reported blocked,
// never identified, however complete its history happens to be.
func TestRealTmuxAdoptedPaneHasNoOriginAndGivesNoID(t *testing.T) {
	socket, target := launchedPane(t, "2000", "24", false, dialogFrame("echo ALPHA"))
	signal, _, info := codexSignalFrom(t, socket, target)
	if info.Origin != "" || signal.State != classifier.StateBlocked || signal.Prompt != nil {
		t.Fatalf("adopted pane: %+v / %+v", info, signal)
	}
}

// The premise Round 5 used, history_size below history_limit, is false:
// tmux trims history in batches. For every limit and number of printed lines,
// the marker is the first captured line exactly when LINE-000 is (ground
// truth from the text), and some of the evicted cases have a scrollback below
// its limit. A dialog with a lookalike heading in its payload that follows
// gets an id only when nothing was evicted.
func TestRealTmuxEvictionAtManyLimitsIsDetectedByTheMarker(t *testing.T) {
	belowLimitButEvicted := 0
	for _, limit := range []int{5, 20, 50} {
		for _, extra := range []int{-4, -1, 0, 1, 2, 4, 9, 14, 30} {
			printed := limit + 6 + extra // rows = 6: the screen holds 6 lines, history the rest
			if printed < 3 {
				continue
			}
			var filler strings.Builder
			for index := 0; index < printed; index++ {
				filler.WriteString("LINE-" + strings.Repeat("0", 3-len(itoa(index))) + itoa(index) + "\n")
			}
			socket, target := launchedPaneSized(t, itoa(limit), "120", "6", true, filler.String()+dialogFrame("echo ALPHA"))
			text, info, err := capturePaneDialog(socket, target)
			if err != nil {
				t.Fatal(err)
			}
			evicted := !strings.Contains(text, "LINE-000")
			if got := startsAtOrigin(text, info); got == evicted {
				t.Fatalf("limit %d printed %d: marker first = %v but LINE-000 evicted = %v", limit, printed, got, evicted)
			}
			signal, _, _ := codexSignalFrom(t, socket, target)
			if (signal.Prompt == nil) != evicted && !strings.Contains(text, "echo ALPHA") {
				// A dialog whose command head was evicted never has an id.
				if signal.Prompt != nil {
					t.Fatalf("limit %d printed %d: id issued for an evicted capture: %+v", limit, printed, signal.Prompt)
				}
			}
			if !evicted && signal.Prompt == nil {
				t.Fatalf("limit %d printed %d: an intact capture was refused: %+v", limit, printed, signal)
			}
			if evicted && signal.Prompt != nil {
				t.Fatalf("limit %d printed %d: LINE-000 and the marker are gone but an id was issued", limit, printed)
			}
			if evicted && sizeBelowLimit(t, socket, target) {
				belowLimitButEvicted++
			}
		}
	}
	if belowLimitButEvicted == 0 {
		t.Fatal("no case reproduced history_size < history_limit with lines evicted; the premise check proves nothing")
	}
	t.Logf("%d capture(s) had history_size < history_limit although lines were evicted", belowLimitButEvicted)
}

func itoa(n int) string { return strconv.Itoa(n) }

func sizeBelowLimit(t *testing.T, socket, target string) bool {
	t.Helper()
	out, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "-t", target, "#{history_size} #{history_limit}").Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(out))
	size, _ := strconv.Atoi(fields[0])
	limit, _ := strconv.Atoi(fields[1])
	return size < limit
}

// Resets: clear-history and an escape that clears the scrollback remove the
// marker, so an ordinary dialog afterwards is not answerable; a resize that
// re-wraps the marker line keeps it first (capture -J joins the wrap).
func TestRealTmuxResetsAndResizeAgainstTheMarker(t *testing.T) {
	socket, target := launchedPane(t, "2000", "24", true, strings.Repeat("filler\n", 40)+dialogFrame("echo ALPHA"))
	signal, text, info := codexSignalFrom(t, socket, target)
	if !startsAtOrigin(text, info) || signal.Prompt == nil {
		t.Fatalf("baseline not answerable: %+v", signal)
	}
	for name, width := range map[string]string{"narrower than the marker": "20", "wider": "200"} {
		if out, err := exec.Command("tmux", "-S", socket, "resize-window", "-t", target, "-x", width).CombinedOutput(); err != nil {
			t.Fatalf("resize: %v: %s", err, out)
		}
		time.Sleep(300 * time.Millisecond)
		text, info, _ := capturePaneDialog(socket, target)
		if !startsAtOrigin(text, info) {
			t.Fatalf("after resizing %s the marker is not the first joined line:\n%q", name, text)
		}
	}
	if out, err := exec.Command("tmux", "-S", socket, "clear-history", "-t", target).CombinedOutput(); err != nil {
		t.Fatalf("clear-history: %v: %s", err, out)
	}
	if signal, text, info := codexSignalFrom(t, socket, target); startsAtOrigin(text, info) || signal.Prompt != nil {
		t.Fatalf("after clear-history an id was issued: %+v", signal)
	}

	// ESC[3J from the program itself.
	socket, target = launchedPane(t, "2000", "24", true, strings.Repeat("filler\n", 40)+"\x1b[3J"+dialogFrame("echo ALPHA"))
	if signal, text, info := codexSignalFrom(t, socket, target); startsAtOrigin(text, info) || signal.Prompt != nil {
		t.Fatalf("after ESC[3J an id was issued: %+v\n%q", signal, text)
	}
}

// What the real capture reports for blanks at a line's end: a space a program
// wrote is kept, and so is the stale extent of a longer line that was
// overwritten in place and erased. The two cannot be told apart, so a dialog
// with such a line followed by more request text is refused, and one drawn
// with whole-line erases (what a redrawing TUI does) is identified.
func TestRealTmuxBlanksAtALineEndAreNotEvidence(t *testing.T) {
	// 1. A command whose quoted string has a written space before its newline.
	written := "OpenAI Codex\n  Would you like to run the following command?\n\n  $ printf 'alpha \n  beta'\n\n› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
	socket, target := launchedPane(t, "2000", "24", true, written)
	text, _, err := capturePaneDialog(socket, target)
	if err != nil || !strings.Contains(text, "printf 'alpha \n") {
		t.Fatalf("tmux did not keep the written space (err %v):\n%q", err, text)
	}
	if signal, _, _ := codexSignalFrom(t, socket, target); signal.State != classifier.StateBlocked || signal.Prompt != nil {
		t.Fatalf("a quoted trailing space was identified: %+v", signal)
	}

	// 2. The same ordinary dialog drawn over longer lines. Erase-to-end-of-line
	// leaves the old extent reported as trailing spaces; a whole-line erase
	// first does not. (The marker stays on the first row: drawing starts at
	// the row below it, from a saved cursor.)
	long := strings.Repeat("x", 50)
	older := "\x1b7" + strings.Repeat(long+"\n", 8)
	draw := func(erase string) string {
		lines := []string{"OpenAI Codex", "  Would you like to run the following command?", "", "  $ echo hi", "", "› 1. Yes, proceed (y)", "  2. No, cancel", "", "  Press enter to confirm or esc to cancel"}
		out := "\x1b8"
		for _, line := range lines {
			if erase == "line" {
				out += "\x1b[2K" + line + "\n"
			} else {
				out += line + "\x1b[K\n"
			}
		}
		return out + "MEWLA-END\n"
	}
	stale, staleTarget := launchedPane(t, "2000", "24", true, older, draw("eol"))
	staleText, _, _ := capturePaneDialog(stale, staleTarget)
	if !strings.Contains(staleText, "  $ echo hi ") {
		t.Fatalf("the fixture left no stale blanks after the command line:\n%q", staleText)
	}
	if signal, _, _ := codexSignalFrom(t, stale, staleTarget); signal.State != classifier.StateBlocked {
		t.Fatalf("stale padding hid the dialog: %+v", signal)
	}
	fresh, freshTarget := launchedPane(t, "2000", "24", true, older, draw("line"))
	if signal, _, _ := codexSignalFrom(t, fresh, freshTarget); signal.Prompt == nil {
		t.Fatalf("a dialog drawn with whole-line erases was refused: %+v", signal)
	}
}

// Blank lines inside the request, in a real -J capture: an empty line, a line
// of spaces and a tab line all refuse the dialog (the Round 5 review's pair
// shared one id); the provider's own separators do not.
func TestRealTmuxBlankLinesInsideTheRequestRefuseTheDialog(t *testing.T) {
	frame := func(inner string) string {
		return "OpenAI Codex\n  Would you like to run the following command?\n\n  $ cat <<'EOF'\n" + inner + "  EOF\n\n" +
			"› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nMEWLA-END\n"
	}
	for name, inner := range map[string]string{"one space": "   \n", "two spaces": "    \n", "empty": "\n", "tab": "  \t\n"} {
		socket, target := launchedPane(t, "2000", "24", true, frame(inner))
		signal, text, info := codexSignalFrom(t, socket, target)
		if !startsAtOrigin(text, info) || signal.State != classifier.StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: %+v", name, signal)
		}
	}
	socket, target := launchedPane(t, "2000", "24", true, frame("  body\n"))
	if signal, _, _ := codexSignalFrom(t, socket, target); signal.Prompt == nil {
		t.Fatalf("an ordinary heredoc was refused: %+v", signal)
	}
}

// The real launch path (CreateSession) makes the marker the pane's first
// output, ahead of whatever the Worker's own command prints, and keeps its
// nonce in the pane's own option; a capture of the new pane starts at it.
func TestRealTmuxCreateSessionPrintsTheOriginMarkerFirst(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "origin-marker", Command: "printf 'FIRST-OUTPUT\\n'; exec /bin/sh", Detached: true, Delegated: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForHarness(t, "the Worker's own output", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, target), "FIRST-OUTPUT")
	})
	text, info, err := capturePaneDialog(h.w.socketPathFor(target), target)
	if err != nil {
		t.Fatal(err)
	}
	if !startsAtOrigin(text, info) || !strings.HasPrefix(info.Origin, "mewla-origin ") || len(info.Origin) != len("mewla-origin ")+24 {
		t.Fatalf("the launched pane's capture does not start at its marker (origin %q):\n%q", info.Origin, text)
	}
	if strings.Index(text, "FIRST-OUTPUT") < strings.Index(text, info.Origin) {
		t.Fatalf("the Worker printed before the marker:\n%q", text)
	}
}

// Real Codex draws on the alternate screen: tmux keeps no scrollback behind it
// (nothing to evict, whatever the limit), the marker printed on the main
// screen is not in the capture, and the capture is the TUI's own screen. The
// real captures of Codex 0.166.0 taken from that screen identify there.
func TestRealTmuxAlternateScreenCaptureIdentifiesARealCodexDialog(t *testing.T) {
	raw, err := os.ReadFile("../classifier/testdata/codex-0.166.0/exec-approval-160x40.txt")
	if err != nil {
		t.Fatal(err)
	}
	frame := "\x1b[?1049h\x1b[H" + strings.TrimRight(string(raw), "\n") + "\nMEWLA-END\n"
	// A tiny scrollback limit would have evicted the marker on the main screen.
	socket, target := launchedPaneSized(t, "4", "160", "40", true, frame)
	text, info, err := capturePaneDialog(socket, target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Alternate || startsAtOrigin(text, info) {
		t.Fatalf("expected the alternate screen without the marker: %+v / %q", info, strings.SplitN(text, "\n", 2)[0])
	}
	text = strings.Replace(text, "MEWLA-END", "", 1)
	signal := classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "codex"}, Capture: &info, PaneContent: text})
	if signal.Prompt == nil || !strings.Contains(signal.Prompt.Request, "echo hello > scratch.txt") {
		t.Fatalf("a real Codex dialog on the alternate screen has no id: %+v", signal)
	}
	// The same bytes on the main screen, with the marker evicted, are refused.
	socket, target = launchedPaneSized(t, "4", "160", "24", true, strings.TrimRight(string(raw), "\n")+"\nMEWLA-END\n")
	text, info, _ = capturePaneDialog(socket, target)
	text = strings.Replace(text, "MEWLA-END", "", 1)
	signal = classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "codex"}, Capture: &info, PaneContent: text})
	if info.Alternate || signal.Prompt != nil {
		t.Fatalf("main screen with an evicted marker: %+v / %+v", info, signal.Prompt)
	}
}
