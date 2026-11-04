package classifier

import (
	"strings"
	"testing"
)

// Prompt identity rests on the whole request. Only provider UI is normalized
// (a menu row's own trailing clock, Codex's status line ahead of the command,
// the provider's terminal countdown notice); every byte of the command stays.

func codexIdentityPane(command, elapsed string) string {
	return "OpenAI Codex\n• Ran ls\n• Working (" + elapsed + " • esc to interrupt)\n\n" +
		"  Would you like to run the following command?\n\n  $ " + command + "\n\n" +
		"› 1. Yes, proceed (y)\n  2. No, cancel\n\n  Press enter to confirm or esc to cancel\n"
}

func codexID(t *testing.T, command, elapsed string) string {
	t.Helper()
	signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: codexIdentityPane(command, elapsed)})
	if signal.State != StateBlocked || signal.Prompt == nil {
		t.Fatalf("no approval prompt for %q: %+v", command, signal)
	}
	return signal.Prompt.ID
}

func TestClockLikeCommandTextKeepsPromptIdentity(t *testing.T) {
	pairs := [][2]string{
		{"printf '(1s • alpha)'", "printf '(2s • beta)'"}, // the Round 2 reproduction
		{"printf '(1s)'", "printf '(2s)'"},
		{"printf '[1m 5s]'", "printf '[2m 5s]'"},
		{"sleep 1 # (1s)", "sleep 1 # (9s)"},
		{"cat <<EOF\n• Working (1s • esc to interrupt)\nEOF", "cat <<EOF\n• Working (2s • esc to interrupt)\nEOF"},
	}
	for _, pair := range pairs {
		first, second := codexID(t, pair[0], "3s"), codexID(t, pair[1], "3s")
		if first == second {
			t.Fatalf("%q and %q share prompt id %s", pair[0], pair[1], first)
		}
		// ...while the provider's own clock still does not move the identity.
		for _, elapsed := range []string{"4s", "59s", "1m 05s"} {
			if got := codexID(t, pair[0], elapsed); got != first {
				t.Fatalf("%q: id moved with the Working clock (%s): %s != %s", pair[0], elapsed, got, first)
			}
		}
	}
}

func TestClockLikeCommandTextKeepsClaudePermissionIdentity(t *testing.T) {
	notice := func(left string) string { return strings.Replace(blockNoticeFmt, "%s", left, 1) }
	pairs := [][2]string{
		{"printf '(1s • alpha)'", "printf '(2s • beta)'"},
		{"printf '(1s)'", "printf '(2s)'"},
		{"echo hi (28s)", "echo hi (29s)"},
	}
	for _, pair := range pairs {
		first := inferPermission(t, heredocPermissionPane(pair[0], notice("2:00"))).ID
		if second := inferPermission(t, heredocPermissionPane(pair[1], notice("2:00"))).ID; second == first {
			t.Fatalf("%q and %q share prompt id %s", pair[0], pair[1], first)
		}
		for _, left := range []string{"1:25", "0:49"} {
			if got := inferPermission(t, heredocPermissionPane(pair[0], notice(left))).ID; got != first {
				t.Fatalf("%q: id moved with the notice clock (%s)", pair[0], left)
			}
		}
	}
}

// A literal countdown notice inside a heredoc is payload; the provider's real
// notice, in its place under the command, is not.
func TestLiteralNoticeInCommandIsKeptNextToTheRealOne(t *testing.T) {
	literal := func(left string) string {
		return "cat > notes.md <<'EOF'\n   ⏱ Claude Code will automatically block this request in " + left + ", to avoid holding up an unattended session\n   EOF"
	}
	real := func(left string) string { return strings.Replace(blockNoticeFmt, "%s", left, 1) }
	first := inferPermission(t, heredocPermissionPane(literal("5 seconds"), real("2:00")))
	if !strings.Contains(first.Request, "in 5 seconds") || strings.Contains(first.Request, "in 2:00") {
		t.Fatalf("literal notice dropped or real one kept: %q", first.Request)
	}
	if other := inferPermission(t, heredocPermissionPane(literal("6 seconds"), real("2:00"))); other.ID == first.ID {
		t.Fatal("two commands that differ in the literal notice share an identity")
	}
	for _, left := range []string{"1:25", "0:49"} {
		if got := inferPermission(t, heredocPermissionPane(literal("5 seconds"), real(left))); got.ID != first.ID {
			t.Fatalf("the real clock (%s) moved the identity", left)
		}
	}
	// No real notice at all: the literal one is still the command's.
	bare := inferPermission(t, heredocPermissionPane(literal("5 seconds"), "(no clock shown)"))
	if !strings.Contains(bare.Request, "in 5 seconds") {
		t.Fatalf("literal notice dropped when it was the only one: %q", bare.Request)
	}
}

// A heredoc line that reads like Claude's banner must not cut the request.
func TestBannerLikeHeredocLineDoesNotTruncateTheRequest(t *testing.T) {
	notice := strings.Replace(blockNoticeFmt, "%s", "2:00", 1)
	for _, banner := range []string{"Claude Code", "Claude Code v9.9.9", "  ▐▛███▜▌   Claude Code v2.1.0"} {
		build := func(head string) string {
			return "rm -rf /tmp/" + head + " && cat > x <<'EOF'\n   " + banner + "\n   body\n   EOF"
		}
		first := inferPermission(t, heredocPermissionPane(build("alpha"), notice))
		if !strings.Contains(first.Request, "/tmp/alpha") || !strings.Contains(first.Request, "body") {
			t.Fatalf("%q: request lost the command above the banner-like line: %q", banner, first.Request)
		}
		if second := inferPermission(t, heredocPermissionPane(build("beta"), notice)); second.ID == first.ID {
			t.Fatalf("%q: commands differing above the banner-like line share an identity", banner)
		}
	}
}

// Without a rule line to bound the request above the cut, nothing proves the
// request is whole: the dialog is still reported, but with no answerable
// prompt.
func TestUnboundedRequestFailsClosed(t *testing.T) {
	pane := "old session output\nClaude Code\n   body\n   EOF\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: pane})
	if signal.State != StateBlocked || signal.Prompt != nil {
		t.Fatalf("want a blocked signal with no prompt, got %+v", signal)
	}
	var long []string
	for index := 0; index < 60; index++ {
		long = append(long, "   line "+strings.Repeat("x", index%7))
	}
	tall := "Claude Code v2.1.0\n" + strings.Join(long, "\n") + "\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	signal = NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: tall})
	if signal.State != StateBlocked || signal.Prompt != nil {
		t.Fatalf("a request longer than the read limit became answerable: %+v", signal)
	}
}

// A very long command is shown trimmed but identified whole.
func TestLongCommandIdentityCoversWhatIsNotShown(t *testing.T) {
	notice := strings.Replace(blockNoticeFmt, "%s", "2:00", 1)
	body := strings.Repeat("echo padding padding padding\n   ", 3)
	tail := strings.Repeat("y", 1900)
	first := inferPermission(t, heredocPermissionPane("echo ONE; "+tail+body, notice))
	second := inferPermission(t, heredocPermissionPane("echo TWO; "+tail+body, notice))
	if first.ID == second.ID {
		t.Fatalf("commands differing only in the part trimmed from the display share an identity")
	}
}
