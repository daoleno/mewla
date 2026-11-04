package classifier

import (
	"strings"
	"testing"
)

// Work 72aa5b49 / %729, 2026-11-05 00:09: a Bash permission for a quoted
// heredoc edit. The notice Claude paints says "block", not "deny", and counts
// down as m:ss. The prompt the Worker's Brain was told about
// (f06d87f7cb85, request "Claude Code will automatically block this request in
// 2:00, ... Do you want to proceed?") was filed while the clock read 2:00, so
// the live re-read at 1:25 had another ID and `worker answer` said "passed".
// Because the notice names "Claude Code", the pane window also began at the
// notice, which dropped the command: the same ID has since filed on dozens of
// unrelated dialogs.
const blockNoticeFmt = "⏱ Claude Code will automatically block this request in %s, to avoid holding up an unattended session"

func heredocPermissionPane(command, notice string) string {
	return "Claude Code v2.1.0\n" +
		"● I'll write the prototype with a quoted heredoc.\n" +
		"────────────────────────────────────────\n" +
		" Bash command\n\n" +
		"   " + strings.ReplaceAll(command, "\n", "\n   ") + "\n" +
		"   Write prototype page\n\n" +
		" Dangerous rm operation not ruled out\n\n" +
		" " + notice + "\n\n" +
		" Do you want to proceed?\n" +
		" ❯ 1. Yes\n" +
		"   2. No\n\n" +
		" Esc to cancel · Tab to amend\n"
}

func inferPermission(t *testing.T, pane string) *Prompt {
	t.Helper()
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: pane})
	if signal.State != StateBlocked || signal.Prompt == nil || signal.Prompt.Kind != PromptPermission {
		t.Fatalf("not a structured permission prompt: %+v\n%s", signal, pane)
	}
	return signal.Prompt
}

func TestBlockCountdownKeepsPermissionIdentityAndCommand(t *testing.T) {
	const command = "cat > /tmp/proto/index.html <<'EOF'"
	first := inferPermission(t, heredocPermissionPane(command, strings.Replace(blockNoticeFmt, "%s", "2:00", 1)))
	if !strings.Contains(first.Request, command) || !strings.Contains(first.Request, "Dangerous rm operation not ruled out") {
		t.Fatalf("the request lost the command or its reason: %q", first.Request)
	}
	if strings.Contains(first.Request, "automatically block") || strings.Contains(first.Request, "unattended") || strings.Contains(first.Request, "2:00") {
		t.Fatalf("the clock reached the request: %q", first.Request)
	}
	for _, left := range []string{"1:25", "0:49", "0:09", "59 seconds", "about 9 seconds", "1 second"} {
		got := inferPermission(t, heredocPermissionPane(command, strings.Replace(blockNoticeFmt, "%s", left, 1)))
		if got.ID != first.ID {
			t.Fatalf("prompt identity changed as the clock ticked to %q: %s != %s", left, got.ID, first.ID)
		}
	}
	// The answer guard still tells a different command, and an expired dialog
	// (no prompt), from the one Brain was told about.
	other := inferPermission(t, heredocPermissionPane("rm -rf /tmp/proto && cat > /tmp/proto/index.html <<'EOF'", strings.Replace(blockNoticeFmt, "%s", "2:00", 1)))
	if other.ID == first.ID {
		t.Fatal("a different command kept the identity")
	}
	if inferred := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: "Claude Code v2.1.0\n● done\n❯ \n"}); inferred.State == StateBlocked {
		t.Fatalf("an expired dialog still blocks: %+v", inferred)
	}
}

func TestBlockCountdownWrappedInNarrowPane(t *testing.T) {
	const command = "cat > /tmp/proto/index.html <<'EOF'"
	wrap := func(text string, width int) string {
		var out []string
		for len(text) > width {
			cut := strings.LastIndex(text[:width], " ")
			out = append(out, text[:cut])
			text = text[cut+1:]
		}
		return strings.Join(append(out, text), "\n ")
	}
	want := inferPermission(t, heredocPermissionPane(command, strings.Replace(blockNoticeFmt, "%s", "2:00", 1))).ID
	for _, width := range []int{100, 70, 50, 40} {
		for _, left := range []string{"2:00", "1:25", "0:49"} {
			notice := wrap(strings.Replace(blockNoticeFmt, "%s", left, 1), width)
			got := inferPermission(t, heredocPermissionPane(command, notice))
			if got.ID != want || !strings.Contains(got.Request, command) || strings.Contains(got.Request, "unattended") {
				t.Fatalf("width %d %q: id %s want %s, request %q", width, left, got.ID, want, got.Request)
			}
		}
	}
}

// A command that says what the notice says is a command, not a clock: it stays
// in the request, and two dialogs that differ only in such a line do not share
// an identity.
func TestNoticeLookalikesInCommandsStayInTheRequest(t *testing.T) {
	notice := strings.Replace(blockNoticeFmt, "%s", "2:00", 1)
	lookalikes := []string{
		"echo 'Claude Code will automatically retry the request' >> notes.md",
		"echo 'Claude Code will automatically block this request in 5 seconds' >> notes.md",
		"# will automatically deny this request",
		"echo automatically deny > policy.txt",
		"⏱ sleep 30 && echo done",
	}
	seen := map[string]string{}
	base := inferPermission(t, heredocPermissionPane("cat > /tmp/proto/index.html <<'EOF'", notice))
	seen[base.ID] = "base"
	for _, line := range lookalikes {
		command := "cat > /tmp/proto/index.html <<'EOF'\n   " + line
		first := inferPermission(t, heredocPermissionPane(command, notice))
		if !strings.Contains(first.Request, line) {
			t.Fatalf("a command line was removed as if it were the notice: %q\nrequest: %q", line, first.Request)
		}
		if strings.Contains(first.Request, "unattended session") {
			t.Fatalf("the real notice leaked beside %q: %q", line, first.Request)
		}
		if previous, clash := seen[first.ID]; clash {
			t.Fatalf("%q collides with %s: %s", line, previous, first.ID)
		}
		seen[first.ID] = line
		// ...and the clock still does not move the identity.
		for _, left := range []string{"1:25", "0:49"} {
			again := inferPermission(t, heredocPermissionPane(command, strings.Replace(blockNoticeFmt, "%s", left, 1)))
			if again.ID != first.ID {
				t.Fatalf("%q: identity moved with the clock (%s): %s != %s", line, left, again.ID, first.ID)
			}
		}
	}
}

// The command's own words about "Claude Code" must not stand in for the banner
// that opens the latest pane window and cut the dialog's request away.
func TestCommandMentioningClaudeCodeKeepsTheDialogWindow(t *testing.T) {
	notice := strings.Replace(blockNoticeFmt, "%s", "1:25", 1)
	command := "git commit -m 'Claude Code will automatically block this request in 2:00'"
	got := inferPermission(t, heredocPermissionPane(command, notice))
	if !strings.Contains(got.Request, command) {
		t.Fatalf("request = %q", got.Request)
	}
}
