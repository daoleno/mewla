package watcher

import "testing"

// A resumed Claude session replays its transcript, so the "Claude Code vX.Y.Z"
// banner scrolls out of the pane while the TUI is fully usable. Captured live
// (2026-10-24) from `claude --resume` of a finished large transcript at 120x40:
// transcript lines, a "✻ … for … · done" summary, the effort status line, an
// empty composer and the auto-mode footer, with no banner anywhere in the pane.
// The transcript text is sanitized; the layout and chrome are the capture's.
//
// The scrollback deliberately keeps words the readiness overlay matcher treats
// as live state ("loading", "esc to interrupt") to prove a resumed pane is
// judged from its current composer onward, not from its replayed history.
const claudeResumedPane = "" +
	"  State left up for resuming: the isolated daemon (port 9890), the emulator paired to it, and the proof pane.\n" +
	"  I've done no merge, release, RESULT.md or cleanup. The worktree holds all the code, and its tests pass.\n" +
	"\n" +
	"✻ Worked for 3m 9s · done 18:01\n" +
	"\n" +
	"❯ Execute:\n" +
	"  Approved option 1: pass the session's gateway variables to the scratch daemon's environment only. Then redo\n" +
	"  the proof, merge (rebase, no force-push) and clean up. No release. Loading the scratch daemon came first.\n" +
	"\n" +
	"  Mewla delegated turn contract:\n" +
	"  Use this turn's identity for every progress command; never reuse an earlier turn's identity.\n" +
	"\n" +
	"● Option 1 it is. I'll restart the scratch daemon with only those two variables added, passing them straight from\n" +
	"  this shell's environment so the token is never printed or written to a file.\n" +
	"\n" +
	"  Ran 1 shell command\n" +
	"\n" +
	"● The new daemon isn't listening. The gate check only tested the tmux invocation, not the daemon, so it doesn't\n" +
	"  count. I'll look at why it didn't start. (esc to interrupt)\n" +
	"\n" +
	"  Ran 1 shell command\n" +
	"\n" +
	"● The old daemon's lifecycle lock hadn't released when the new one started. Nothing is running now, so I'll start\n" +
	"  it again.\n" +
	"\n" +
	"  Ran 1 shell command\n" +
	"                                                                                                    ◐ medium · /effort\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"❯\u00a0\n" +
	"────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────\n" +
	"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n"

func TestClaudeResumedPaneWithoutHeaderIsInputReady(t *testing.T) {
	if claudeHeaderRe.MatchString(claudeResumedPane) {
		t.Fatal("fixture must not contain the Claude banner; a resumed pane scrolls it away")
	}
	if !isClaudeInputReady(claudeResumedPane) {
		t.Fatal("resumed Claude pane with an empty composer and mode footer should be input-ready")
	}
}

func TestClaudeResumedPaneBlockedStatesAreNotReady(t *testing.T) {
	for name, pane := range map[string]string{
		"draft in composer": "" +
			"✻ Worked for 3m 9s · done 18:01\n" +
			"❯ Execute the approved option\n" +
			"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n",
		"footer above composer": "" +
			"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n" +
			"❯\u00a0\n",
		"busy footer": "" +
			"✻ Worked for 3m 9s · done 18:01\n" +
			"❯\u00a0\n" +
			"  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt · ← for agents\n",
		"retry after composer": "" +
			"✻ Worked for 3m 9s · done 18:01\n" +
			"❯\u00a0\n" +
			"✻ Connection refused — retrying\n" +
			"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n",
		"trust prompt": "" +
			"Do you trust the files in this folder?\n" +
			"❯ 1. Yes\n" +
			"  2. No\n" +
			"  ⏵⏵ auto mode on (shift+tab to cycle)\n",
	} {
		if isClaudeInputReady(pane) {
			t.Errorf("%s must not be input-ready", name)
		}
	}
}
