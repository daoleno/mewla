package classifier

import (
	"strings"
	"testing"
)

// The exact-identity invariants (see prompt_dialog.go): two dialogs that ask
// for different things never share an id, whatever the difference is made of.

func claudeExactID(t *testing.T, command string) (string, string) {
	t.Helper()
	got := inferPermission(t, heredocPermissionPane(command, strings.Replace(blockNoticeFmt, "%s", "2:00", 1)))
	return got.ID, got.Request
}

func codexExactSignal(command string) ActivitySignal {
	return NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: codexIdentityPane(command, "3s")})
}

func codexExactID(t *testing.T, command string) string {
	t.Helper()
	signal := codexExactSignal(command)
	if signal.State != StateBlocked || signal.Prompt == nil {
		t.Fatalf("no approval prompt for %q: %+v", command, signal)
	}
	return signal.Prompt.ID
}

func TestExactIdentityKeepsPayloadWhitespaceAndLength(t *testing.T) {
	pairs := [][2]string{
		{"printf 'a b'", "printf 'a  b'"},
		{"printf 'a b'", "printf 'a\tb'"},
		{"cat <<'EOF'\n  indented\nEOF", "cat <<'EOF'\n    indented\nEOF"},
		{"cat <<'EOF'\na\n\nb\nEOF", "cat <<'EOF'\na\n\n\nb\nEOF"},
		{"echo hi", "echo hi "},
	}
	for _, pair := range pairs[:4] { // a trailing space at a line's end is not visible in a capture
		claudeFirst, _ := claudeExactID(t, pair[0])
		if claudeSecond, _ := claudeExactID(t, pair[1]); claudeSecond == claudeFirst {
			t.Fatalf("claude: %q and %q share id %s", pair[0], pair[1], claudeFirst)
		}
		if codexFirst, codexSecond := codexExactID(t, pair[0]), codexExactID(t, pair[1]); codexFirst == codexSecond {
			t.Fatalf("codex: %q and %q share id %s", pair[0], pair[1], codexFirst)
		}
	}
}

func commonTail(lines int) string {
	return strings.Repeat("\n# common comment line", lines)
}

func TestExactIdentityCoversAnOperationBeforeTheLineLimit(t *testing.T) {
	alpha, beta := "echo ALPHA"+commonTail(45), "echo BETA"+commonTail(45)
	first, request := claudeExactID(t, alpha)
	if second, _ := claudeExactID(t, beta); second == first {
		t.Fatalf("claude: commands that differ before the line limit share id %s", first)
	}
	if !strings.Contains(request, "common comment line") {
		t.Fatalf("claude request lost its body: %q", request)
	}
	if a, b := codexExactID(t, alpha), codexExactID(t, beta); a == b {
		t.Fatalf("codex: commands that differ before the line limit share id %s", a)
	}
}

func TestDialogWhoseStartIsNotInTheCaptureHasNoAnswerableID(t *testing.T) {
	// The capture begins inside the command: no rule line / heading sentence above.
	claude := "   echo THE-REST\n   Write prototype page\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: claude})
	if signal.State != StateBlocked || signal.Prompt != nil {
		t.Fatalf("claude: %+v", signal)
	}
	codex := "OpenAI Codex\n  $ echo THE-REST\n\n› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\nWould you like to run the following command?\n"
	signal = NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: codex})
	if signal.State != StateBlocked || signal.Prompt != nil {
		t.Fatalf("codex: %+v", signal)
	}
}

// A banner, a composer, a heading or a status line inside a command is the
// command's, and what comes before it still counts.
func TestExactIdentityIgnoresProviderLookalikesInThePayload(t *testing.T) {
	lookalikes := []string{
		"OpenAI Codex",
		"│ >_ OpenAI Codex (v0.46.0) │",
		"› typed like a composer",
		"Would you like to run the following command?",
		"• Working (1s • esc to interrupt)",
		"Claude Code v9.9.9",
		"-----------------------------",
		"   ──────────────────────────",
	}
	for _, line := range lookalikes {
		build := func(head string) string { return "rm -rf /tmp/" + head + " && cat <<'EOF'\n" + line + "\nbody\nEOF" }
		if a, b := codexExactID(t, build("alpha")), codexExactID(t, build("beta")); a == b {
			t.Fatalf("codex: %q: commands differing above it share id %s", line, a)
		}
		first, request := claudeExactID(t, build("alpha"))
		if second, _ := claudeExactID(t, build("beta")); second == first {
			t.Fatalf("claude: %q: commands differing above it share id %s", line, first)
		}
		if !strings.Contains(request, "/tmp/alpha") {
			t.Fatalf("claude: %q: the request lost the head of the command: %q", line, request)
		}
		signal := codexExactSignal(build("alpha"))
		if signal.Prompt == nil || !strings.Contains(signal.Prompt.Request, "/tmp/alpha") {
			t.Fatalf("codex: %q: the request lost the head of the command: %+v", line, signal.Prompt)
		}
	}
}

// Status-shaped lines in a payload that has no shell "$" line (a tool call's
// arguments) are payload too.
func TestStatusShapedLinesInAToolCallPayloadAreKept(t *testing.T) {
	pane := func(elapsed string) string {
		return "OpenAI Codex\n  Allow the wechat MCP server to run tool \"send\"?\n\n  text: • Working (" + elapsed + " • esc to interrupt)\n\n" +
			"› 1. Allow\n  2. Cancel\n\n  Press enter to confirm or esc to cancel\n"
	}
	id := func(elapsed string) string {
		signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: pane(elapsed)})
		if signal.Prompt == nil {
			t.Fatalf("no prompt: %+v", signal)
		}
		return signal.Prompt.ID
	}
	if id("1s") == id("2s") {
		t.Fatal("tool-call payloads that differ only in a status-shaped number share an id")
	}
}
