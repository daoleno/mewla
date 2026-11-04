package classifier

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Captures of the installed Codex client (0.166.0) showing a real approval:
// taken with `tmux capture-pane -p -J` of its alternate-screen TUI, scratch path
// replaced by /tmp/scratch. They carry the renderer's habits that fixtures drawn
// by hand do not: painted blanks of arbitrary width after text and on rows that
// are blank in the UI, an Environment row and a Reason row ahead of the command,
// a menu row Codex wraps itself, and, for a long command, Codex's own cut.

func realCodexCapture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/codex-0.166.0/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var altScreenCapture = &CaptureInfo{Exact: true, Alternate: true}

func codexOnRealCapture(pane string, capture *CaptureInfo) ActivitySignal {
	return NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: capture, PaneContent: pane})
}

func TestRealCodexApprovalIsAnswerableOnItsAlternateScreen(t *testing.T) {
	for _, name := range []string{"exec-approval-160x40.txt", "exec-approval-80x24-wrapped-menu.txt"} {
		signal := codexOnRealCapture(realCodexCapture(t, name), altScreenCapture)
		if signal.State != StateBlocked || signal.Prompt == nil {
			t.Fatalf("%s: a real approval has no prompt: %+v", name, signal)
		}
		if !strings.Contains(signal.Prompt.Request, "echo hello > scratch.txt") || len(signal.Prompt.Options) != 3 || signal.Prompt.Options[0].Number != 1 {
			t.Fatalf("%s: prompt %+v", name, signal.Prompt)
		}
	}
}

// The main-screen proof (the origin marker) cannot exist on the alternate
// screen: without either proof the real dialog is reported with no id.
func TestRealCodexApprovalNeedsAProofOfItsStart(t *testing.T) {
	pane := realCodexCapture(t, "exec-approval-160x40.txt")
	for name, capture := range map[string]*CaptureInfo{
		"no capture":                  nil,
		"not exact":                   {Alternate: true},
		"main screen, no marker":      {Exact: true},
		"main screen, foreign marker": {Exact: true, Origin: "mewla-origin ffffffffffffffffffffffff"},
	} {
		if signal := codexOnRealCapture(pane, capture); signal.State != StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: %+v", name, signal)
		}
	}
}

// Codex cuts a long command to fit ("[… 63 lines] ^a view all"): the screen
// never holds the whole request, so two commands that differ only in the cut
// part would share one id. No id is issued.
func TestRealCodexTruncatedCommandHasNoID(t *testing.T) {
	signal := codexOnRealCapture(realCodexCapture(t, "exec-approval-truncated-160x40.txt"), altScreenCapture)
	if signal.State != StateBlocked || signal.Prompt != nil {
		t.Fatalf("a command Codex cut to fit was identified: %+v", signal.Prompt)
	}
}

var trailingBlanksRe = regexp.MustCompile(`[ \t]+$`)

// The renderer's padding is not evidence: rows outside the command carry
// arbitrary trailing blanks, and the id does not move with them.
func TestRealCodexIDIgnoresThePaddingOfProviderRows(t *testing.T) {
	pane := realCodexCapture(t, "exec-approval-160x40.txt")
	base := codexOnRealCapture(pane, altScreenCapture).Prompt
	if base == nil {
		t.Fatal("no prompt on the real capture")
	}
	var lines []string
	for index, line := range strings.Split(pane, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "$ ") {
			line = trailingBlanksRe.ReplaceAllString(line, "") + strings.Repeat(" ", (index*7)%43)
		}
		lines = append(lines, line)
	}
	repadded := codexOnRealCapture(strings.Join(lines, "\n"), altScreenCapture).Prompt
	if repadded == nil || repadded.ID != base.ID {
		t.Fatalf("padding moved the id: %v vs %s", repadded, base.ID)
	}
	other := codexOnRealCapture(strings.Replace(pane, "$ echo hello > scratch.txt\n", "$ echo hello > scratch2.txt\n", 1), altScreenCapture).Prompt
	if other == nil || other.ID == base.ID {
		t.Fatalf("a different command shares the id: %+v", other)
	}
}

// A dialog with no `$` line (a network or MCP question) has no known layout:
// its body is the request, and a blank row or trailing blanks inside it refuse
// the dialog. This is the MCP write_file pair of the Round 6 review.
func TestCodexDialogWithoutACommandLineRefusesAmbiguousBlanks(t *testing.T) {
	dialog := func(inner string) string {
		return "  Allow the notes MCP server to run tool write_file?\n\n  content:\n" + inner + "  end of content\n\n› 1. Allow\n  2. Cancel\n\n  Press enter to confirm\n"
	}
	for name, inner := range map[string]string{
		"one space":        " \n",
		"two spaces":       "  \n",
		"empty":            "\n",
		"tab":              "\t\n",
		"text then spaces": "  keep this \n",
	} {
		if signal := codexOnRealCapture(dialog(inner), altScreenCapture); signal.State != StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: got a prompt %+v", name, signal.Prompt)
		}
	}
	if signal := codexOnRealCapture(dialog("  keep this\n"), altScreenCapture); signal.Prompt == nil {
		t.Fatalf("a body without blanks was refused: %+v", signal)
	}
	network := "  Do you want to approve network access to \"example.com\"?\n\n  Reason: network request blocked\n\n› 1. Yes, just this once (y)\n  2. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel\n"
	if signal := codexOnRealCapture(network, altScreenCapture); signal.Prompt == nil {
		t.Fatalf("the network approval was refused: %+v", signal)
	}
}

// Inside the command, as before: trailing blanks before a line break and blank
// rows refuse the dialog (the Round 5 and 6 pairs), on the real layout.
func TestRealCodexLayoutKeepsTheBlankRulesInsideTheCommand(t *testing.T) {
	pane := realCodexCapture(t, "exec-approval-160x40.txt")
	for name, command := range map[string]string{
		"trailing space before a line break": "printf 'alpha \n  beta'",
		"two trailing spaces":                "printf 'alpha  \n  beta'",
		"one space on a line":                "cat <<'EOF'\n   \n  EOF",
		"two spaces on a line":               "cat <<'EOF'\n    \n  EOF",
		"an empty line":                      "cat <<'EOF'\n\n  EOF",
	} {
		changed := strings.Replace(pane, "$ echo hello > scratch.txt\n", "$ "+command+"\n", 1)
		if signal := codexOnRealCapture(changed, altScreenCapture); signal.State != StateBlocked || signal.Prompt != nil {
			t.Fatalf("%s: got a prompt %+v", name, signal.Prompt)
		}
	}
}
