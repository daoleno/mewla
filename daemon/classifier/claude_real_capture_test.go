package classifier

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Captures of the installed Claude client (2.1.318) with a permission dialog
// open, taken with `tmux capture-pane -p -J -S -` of its alternate-screen TUI
// (alternate_on=1, history_size=0: the capture is the screen and nothing else),
// scratch path replaced by /tmp/scratch. They carry what a hand-drawn dialog
// does not:
//   - the box is drawn as a header, a `╌` separator, the request on rows with a
//     " │ " gutter (a single-line command has no gutter), a `╌` separator and
//     the warning rows;
//   - Claude paints trailing blanks of arbitrary width after rows, including
//     rows of the request ("systemctl status foo --no-pager" + 9 blanks that
//     are not in the command) and the row of an empty command line;
//   - a request taller than the pane is not cut by Claude: the box grows and
//     its head, rule included, scrolls off the screen.
// The two dialogs of the 2026-11-05 incident (a long multi-line ssh command and
// a "Dangerous rm operation on possibly-empty variable path" warning) were
// refused with prompt_refused because of the painted blanks.

func realClaudeCapture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/claude-2.1.318/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var claudeAltScreen = &CaptureInfo{Exact: true, Alternate: true}

func claudeOnRealCapture(pane string) ActivitySignal {
	return NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: claudeAltScreen, PaneContent: pane})
}

func TestRealClaudePermissionDialogsAreAnswerable(t *testing.T) {
	for name, want := range map[string]struct {
		options int
		request string
	}{
		"bash-multiline-ssh.txt": {3, "systemctl status foo --no-pager"},
		"dangerous-rm.txt":       {2, "Dangerous rm operation on possibly-empty variable path"},
	} {
		signal := claudeOnRealCapture(realClaudeCapture(t, name))
		if signal.State != StateBlocked || signal.Prompt == nil || signal.Unidentified != "" {
			t.Fatalf("%s: a real open dialog has no prompt (%q): %+v", name, signal.Unidentified, signal)
		}
		prompt := signal.Prompt
		if len(prompt.Options) != want.options || prompt.Options[0].Number != 1 || prompt.Kind != PromptPermission || prompt.ID == "" {
			t.Fatalf("%s: prompt %+v", name, prompt)
		}
		if !strings.Contains(prompt.Request, want.request) {
			t.Fatalf("%s: request %q lacks %q", name, prompt.Request, want.request)
		}
	}
}

func realClaudeID(t *testing.T, pane string) string {
	t.Helper()
	signal := claudeOnRealCapture(pane)
	if signal.Prompt == nil {
		t.Fatalf("no prompt (%q) for:\n%s", signal.Unidentified, pane)
	}
	return signal.Prompt.ID
}

// The same open dialog read again must keep its id however Claude pads the
// rows, where its cursor stands, and whatever its spinner above does: an id
// that moves with the padding is a prompt that has "passed" at answer time.
func TestRealClaudeDialogIdentityIgnoresPaintingAndCursor(t *testing.T) {
	for _, name := range []string{"bash-multiline-ssh.txt", "dangerous-rm.txt"} {
		pane := realClaudeCapture(t, name)
		id := realClaudeID(t, pane)
		lines := strings.Split(pane, "\n")
		padded := make([]string, len(lines))
		for index, line := range lines {
			padded[index] = strings.TrimRight(line, " ") + strings.Repeat(" ", (index*7)%23)
		}
		if got := realClaudeID(t, strings.Join(padded, "\n")); got != id {
			t.Fatalf("%s: other painted blanks changed the id %s -> %s", name, id, got)
		}
		stripped := make([]string, len(lines))
		for index, line := range lines {
			stripped[index] = strings.TrimRight(line, " ")
		}
		if got := realClaudeID(t, strings.Join(stripped, "\n")); got != id {
			t.Fatalf("%s: no painted blanks changed the id %s -> %s", name, id, got)
		}
		moved := strings.Replace(strings.Replace(pane, "❯ 1.", "  1.", 1), "  2. ", "❯ 2. ", 1)
		if moved == pane {
			t.Fatalf("%s: fixture has no cursor to move", name)
		}
		if got := realClaudeID(t, moved); got != id {
			t.Fatalf("%s: the cursor moving changed the id %s -> %s", name, id, got)
		}
		// Above the dialog is conversation: its spinner is not the dialog.
		spun := strings.Replace(pane, "Deleting .log files", "Deleting .log  files", 1)
		spun = strings.Replace(spun, "Ran 1 shell command", "Running...", 1)
		if got := realClaudeID(t, spun); got != id {
			t.Fatalf("%s: text above the dialog changed the id %s -> %s", name, id, got)
		}
	}
}

// Stale/different dialog: a command that differs by one character is another
// request, and so is any other line of the dialog.
func TestRealClaudeDialogIdentityTellsRequestsApart(t *testing.T) {
	pane := realClaudeCapture(t, "bash-multiline-ssh.txt")
	id := realClaudeID(t, pane)
	lines := strings.Split(pane, "\n")
	for name, mutate := range map[string]func(string) string{
		"one character of the command": func(s string) string { return strings.Replace(s, "ls -la /tmp/$f", "ls -la /tmp/$g", 1) },
		"the host":                     func(s string) string { return strings.Replace(s, "nobody@192.0.2.1", "nobody@192.0.2.2", 2) },
		"a line of the command":        func(s string) string { return strings.Replace(s, " │ cd /tmp", " │ cd /etc", 1) },
		"an indent in the command": func(s string) string {
			return strings.Replace(s, " │   echo \\\"checking", " │ echo \\\"checking", 1)
		},
		"a removed empty command line": func(s string) string { return strings.Replace(s, " │ \n", "", 1) },
		"the tool header":              func(s string) string { return strings.Replace(s, " Bash command\n", " PowerShell command\n", 1) },
		"the menu":                     func(s string) string { return strings.Replace(s, "3. No", "3. Yes, always", 1) },
	} {
		// The mutation applies to the dialog, not to the copy of the command in
		// the conversation above it.
		mutated := strings.Join(lines[:17], "\n") + "\n" + mutate(strings.Join(lines[17:], "\n"))
		if mutated == pane {
			// the empty command row carries only blanks in the capture
			mutated = regexp.MustCompile(`(?m)^ │ +\n`).ReplaceAllString(pane, "")
			if mutated == pane || name != "a removed empty command line" {
				t.Fatalf("%s: mutation did not apply", name)
			}
		}
		if got := realClaudeID(t, mutated); got == id {
			t.Fatalf("%s: a different dialog kept the id %s", name, id)
		}
	}
}

// Without the rule and header that open the box nothing proves where the
// dialog starts: it is still blocked, with no id and a reason.
func TestRealClaudeDialogWithoutItsStartHasNoID(t *testing.T) {
	pane := realClaudeCapture(t, "bash-multiline-ssh.txt")
	lines := strings.Split(pane, "\n")
	for name, cut := range map[string]string{
		"rule removed":             strings.Join(append(append([]string{}, lines[:17]...), lines[18:]...), "\n"),
		"cut inside the command":   strings.Join(lines[22:], "\n"),
		"cut at the header":        strings.Join(lines[18:], "\n"),
		"rule then only the box":   strings.Join(append(append([]string{}, lines[:18]...), lines[21:]...), "\n"),
		"separator in place of it": strings.Replace(pane, lines[17], lines[21], 1),
	} {
		signal := claudeOnRealCapture(cut)
		if signal.State != StateBlocked {
			t.Fatalf("%s: not blocked: %+v", name, signal)
		}
		if signal.Prompt != nil || signal.Unidentified == "" {
			t.Fatalf("%s: got an id for a dialog whose start is not shown: %+v (%q)", name, signal.Prompt, signal.Unidentified)
		}
	}
}

// A request taller than the pane scrolls its head off the alternate screen (no
// history behind it). Whatever is left cannot be told from another request
// with the same tail, so it is reported with no id and the reason, never hashed.
func TestRealClaudeRequestTallerThanThePaneHasNoID(t *testing.T) {
	for _, name := range []string{"tall-60-lines-40x40.txt", "tall-60-lines-100x24.txt"} {
		signal := claudeOnRealCapture(realClaudeCapture(t, name))
		if signal.State != StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: %+v", name, signal)
		}
		if !strings.Contains(signal.Unidentified, "where the dialog starts") {
			t.Fatalf("%s: reason %q", name, signal.Unidentified)
		}
	}
}

// A line of dashes in the request is text, not the rule that opens the box: it
// neither shortens the identity to the part below it nor makes up a start.
func TestRealClaudeForgedRuleInTheRequestDoesNotShortenTheID(t *testing.T) {
	pane := realClaudeCapture(t, "dangerous-rm.txt")
	forged := strings.Replace(pane, " rm -f $UNSETDIRVAR/*.log\n", " ────────────────────\n", 1)
	if forged == pane {
		t.Fatal("fixture: no command row")
	}
	// Two dialogs that differ only above the forged rule: the description under
	// the real header.
	other := strings.Replace(forged, "Delete .log files in the directory named by a variable", "Remove the configured scratch files", 1)
	if other == forged {
		t.Fatal("fixture: no description row")
	}
	a, b := claudeOnRealCapture(forged), claudeOnRealCapture(other)
	if a.Prompt == nil || b.Prompt == nil {
		t.Fatalf("the real dialog above a forged rule lost its start: %+v %+v", a, b)
	}
	if a.Prompt.ID == b.Prompt.ID {
		t.Fatalf("a rule-looking command line cut the identity: both %s", a.Prompt.ID)
	}
	// In the gutter rows of a multi-line request a dash row is not a rule at all.
	multi := realClaudeCapture(t, "bash-multiline-ssh.txt")
	multi = strings.Replace(multi, " │ cd /tmp", " │ ──────────────────\n │ Bash command\n │ cd /tmp", 1)
	if signal := claudeOnRealCapture(multi); signal.Prompt == nil || !strings.Contains(signal.Prompt.Request, "ssh -o BatchMode") {
		t.Fatalf("dash row inside the request: %+v", signal)
	}
}

// Claude wraps a long single-line command itself, and the wrapped rows start
// with a space like every other row of the box. A run of dashes in the
// command that lands on its own wrapped row, followed by text with letters,
// looks like "a rule and a header" one row apart; only the rule at column 0
// opens the dialog. Commands that differ before the dashes must not share an
// id (or must have none).
func TestRealClaudeWrappedDashesInASingleLineCommandDoNotStartTheDialog(t *testing.T) {
	pane := realClaudeCapture(t, "dangerous-rm.txt")
	lines := strings.Split(pane, "\n")
	build := func(head string) string {
		out := append([]string{}, lines[:26]...) // up to and including the real rule
		out = append(out, lines[26], lines[27], lines[28])
		out = append(out, " "+head, " "+strings.Repeat("─", 100), " next")
		out = append(out, lines[30:]...)
		return strings.Join(out, "\n")
	}
	if !strings.HasPrefix(lines[26], "─") || !strings.Contains(lines[27], "Bash command") {
		t.Fatalf("fixture shape: %q %q", lines[26], lines[27])
	}
	a, b := claudeOnRealCapture(build("echo AAAA")), claudeOnRealCapture(build("echo BBBB"))
	if a.State != StateBlocked || b.State != StateBlocked {
		t.Fatalf("not blocked: %+v %+v", a, b)
	}
	if a.Prompt == nil || b.Prompt == nil {
		t.Fatalf("the real rule above the command was lost: %+v %+v", a.Prompt, b.Prompt)
	}
	if a.Prompt.ID == b.Prompt.ID || !strings.Contains(a.Prompt.Request, "echo AAAA") {
		t.Fatalf("commands that differ before the wrapped dashes share an id: %s / %s", a.Prompt.ID, b.Prompt.ID)
	}
	// No real rule above: the wrapped dashes must not become the start.
	headless := strings.Join(strings.Split(build("echo AAAA"), "\n")[27:], "\n")
	if signal := claudeOnRealCapture(headless); signal.Prompt != nil {
		t.Fatalf("wrapped dashes became the dialog start: %+v", signal.Prompt)
	}
}

// The countdown notice Brain quoted ("⚠ … automatically block this request in
// 1:52") counts down while the dialog is open. Its clock must not be in the
// id: an id that changes every second is prompt_gone by the time it is used.
func TestRealClaudeCountdownNoticeDoesNotMoveTheID(t *testing.T) {
	pane := realClaudeCapture(t, "bash-multiline-ssh.txt")
	lines := strings.Split(pane, "\n")
	// after the warning row (line 32), before the blank row and the question
	withNotice := func(prefix, clock string) string {
		notice := []string{prefix + "⚠ Claude Code will automatically block this request in " + clock + ", to avoid holding up an unattended session"}
		out := append(append(append([]string{}, lines[:33]...), notice...), lines[33:]...)
		return strings.Join(out, "\n")
	}
	for name, prefix := range map[string]string{"no gutter": " ", "gutter": " │ "} {
		a := claudeOnRealCapture(withNotice(prefix, "1:52"))
		b := claudeOnRealCapture(withNotice(prefix, "1:51"))
		if a.Prompt == nil || b.Prompt == nil {
			t.Fatalf("%s: no prompt: %q %q", name, a.Unidentified, b.Unidentified)
		}
		if a.Prompt.ID != b.Prompt.ID {
			t.Fatalf("%s: the clock moved the id %s -> %s", name, a.Prompt.ID, b.Prompt.ID)
		}
		if strings.Contains(a.Prompt.Request, "automatically block") {
			t.Fatalf("%s: the notice is in the request Brain reads: %q", name, a.Prompt.Request)
		}
	}
	// The notice is volatile text, not the command: another command still differs.
	base := withNotice(" ", "1:52")
	cut := strings.LastIndex(base, "systemctl status foo")
	other := base[:cut] + "systemctl status fob" + base[cut+len("systemctl status foo"):]
	if claudeOnRealCapture(other).Prompt.ID == claudeOnRealCapture(base).Prompt.ID {
		t.Fatal("a different command shares the id with a notice present")
	}
}
