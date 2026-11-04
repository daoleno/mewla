package classifier

import (
	"strings"
	"testing"
)

// The pane Brain saw on %790 and %721: ordinary tool output (a `mewla
// connections --help` body that says "--allow grants future calls to that
// tool") above Codex's idle-looking composer. Nothing is waiting for anyone.
const codexHelpOutputPane = "• Ran mewla connections --help\n" +
	"  └ policy --id account --tool name --allow grants future calls to that tool; only change on explicit user consent.\n" +
	"    policy --id account --tools a,b,c [--allow] sets every listed tool at once, all or none; same explicit consent.\n" +
	"    Never send messages or make updates without task-specific user authorization, even when a tool is enabled.\n" +
	"• Working (32s • esc to interrupt)\n" +
	"› Ask Codex to do anything\n" +
	"  GPT-6-Astra max · ~\n"

const codexRunCommandApproval = "  Would you like to run the following command?\n\n" +
	"  $ systemctl list-unit-files --state=enabled --no-pager\n\n" +
	"› 1. Yes, proceed (y)\n" +
	"  2. Yes, and don't ask again for commands that start with `systemctl` (p)\n" +
	"  3. No, and tell Codex what to do differently (esc)\n\n" +
	"  Press enter to confirm or esc to cancel\n"

const codexTrustMenu = "  You are in /workspace/future\n\n" +
	"  Do you trust the contents of this directory? Working with untrusted contents comes with higher risk of prompt injection.\n\n" +
	"› 1. Yes, continue\n" +
	"  2. No, quit\n\n" +
	"  Press enter to continue\n"

const codexMCPApproval = "  Allow the wechat MCP server to run tool \"send_message\"?\n\n" +
	"› 1. Allow\n" +
	"  2. Allow for this session\n" +
	"  3. Always allow\n" +
	"  4. Cancel\n\n" +
	"  Press enter to confirm or esc to cancel\n"

func codexPane(header bool, parts ...string) string {
	pane := strings.Join(parts, "")
	if header {
		return testOrigin + "\nOpenAI Codex\n" + pane
	}
	return testOrigin + "\n" + pane
}

func TestCodexOrdinaryOutputDoesNotRaiseApproval(t *testing.T) {
	cases := map[string]string{
		"help text above placeholder composer":       codexHelpOutputPane,
		"do you want to quoted in tool output":       "• Ran ./installer\n  └ Do you want to continue? is the question the installer asks\n• Working (4s • esc to interrupt)\n› Ask Codex to do anything\n  GPT-6-Astra max · ~\n",
		"allow x to quoted in tool output":           "• Ran rg allow\n  └ Allow the proxy to forward requests; would you like to run it?\n• Working (4s • esc to interrupt)\n› Ask Codex to do anything\n  GPT-6-Astra max · ~\n",
		"user draft in the composer":                 "• Ran ls\n  └ Do you want to proceed?\n› fix the allow list to include the new host\n  GPT-6-Astra max · ~\n",
		"approval phrase in a transcript, idle bare": "• Ran ls\n  └ Press enter to continue\n› \n  GPT-6-Astra max · ~\n",
	}
	for name, body := range cases {
		for _, header := range []bool{true, false} {
			pane := codexPane(header, body)
			label := name
			if header {
				label += "/header"
			}
			t.Run(label, func(t *testing.T) {
				signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: exactCaptureInfo, PaneContent: pane})
				if signal.State == StateBlocked || signal.Prompt != nil {
					t.Fatalf("adapter blocked ordinary output: %#v", signal)
				}
				state, summary := Classify(true, strings.Split(pane, "\n"), "codex")
				if state == StateBlocked {
					t.Fatalf("Classify blocked ordinary output: %q", summary)
				}
			})
		}
	}
}

// The help-text pane is still a Working pane: the fix must restore Running
// evidence, not merely drop the false block.
func TestCodexHelpOutputPaneReportsWorking(t *testing.T) {
	signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: exactCaptureInfo, PaneContent: "OpenAI Codex\n" + codexHelpOutputPane})
	if signal.State != StateRunning || signal.Source != "codex_pane_working" {
		t.Fatalf("signal = %#v, want running codex_pane_working", signal)
	}
}

func TestCodexRealApprovalsStillBlockWithPrompt(t *testing.T) {
	history := "• Ran mewla connections --help\n" +
		"  └ policy --id account --tool name --allow grants future calls to that tool; only change on explicit user consent.\n" +
		"• Working (3s • esc to interrupt)\n\n"
	cases := []struct {
		name    string
		pane    string
		options []string
	}{
		{"run command", codexRunCommandApproval, []string{"Yes, proceed (y)", "Yes, and don't ask again for commands that start with `systemctl` (p)", "No, and tell Codex what to do differently (esc)"}},
		{"run command after help output and Working line", history + codexRunCommandApproval, []string{"Yes, proceed (y)", "Yes, and don't ask again for commands that start with `systemctl` (p)", "No, and tell Codex what to do differently (esc)"}},
		{"trust directory", codexTrustMenu, []string{"Yes, continue", "No, quit"}},
		{"mcp tool approval after allow-to history", history + codexMCPApproval, []string{"Allow", "Allow for this session", "Always allow", "Cancel"}},
	}
	for _, tc := range cases {
		for _, header := range []bool{true, false} {
			pane := codexPane(header, tc.pane)
			label := tc.name
			if header {
				label += "/header"
			}
			t.Run(label, func(t *testing.T) {
				signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: exactCaptureInfo, PaneContent: pane})
				if signal.State != StateBlocked || signal.Source != "codex_pane_blocked" {
					t.Fatalf("adapter lost the approval: %#v", signal)
				}
				if signal.Prompt == nil {
					t.Fatalf("approval has no parsed prompt: %#v", signal)
				}
				var got []string
				for _, option := range signal.Prompt.Options {
					got = append(got, option.Label)
				}
				if strings.Join(got, "|") != strings.Join(tc.options, "|") {
					t.Fatalf("options = %q, want %q", got, tc.options)
				}
				state, _ := Classify(true, strings.Split(pane, "\n"), "codex")
				if state != StateBlocked {
					t.Fatalf("Classify state = %q, want blocked", state)
				}
			})
		}
	}
}

// A codex pane whose process name is not "codex" (a node wrapper) is still
// recognized by its chrome for the adapter; Classify sees only lines+command.
func TestClassifyRecognizesCodexByChromeWhenCommandIsWrapped(t *testing.T) {
	pane := "OpenAI Codex\n" + codexHelpOutputPane
	state, summary := Classify(true, strings.Split(pane, "\n"), "node")
	if state == StateBlocked {
		t.Fatalf("wrapped codex help output blocked: %q", summary)
	}
}

func TestCodexMenuLessApprovalPromptStillBlocks(t *testing.T) {
	signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: exactCaptureInfo, PaneContent: "OpenAI Codex\nDo you want to continue?"})
	if signal.State != StateBlocked {
		t.Fatalf("signal = %#v", signal)
	}
}

// Codex counts its Working line up while a command waits for approval. That
// clock must not become part of the prompt's identity, or `worker answer`
// finds the dialog "passed" a second after Brain was told of it.
func TestCodexApprovalIdentityIgnoresWorkingClock(t *testing.T) {
	pane := func(command, elapsed string) string {
		return testOrigin + "\nOpenAI Codex\n• Ran ls\n• Working (" + elapsed + " • esc to interrupt)\n\n" +
			"  Would you like to run the following command?\n\n  $ " + command + "\n\n" +
			"› 1. Yes, proceed (y)\n  2. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel\n"
	}
	infer := func(command, elapsed string) *Prompt {
		t.Helper()
		signal := NewCodexActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "codex"}, Capture: exactCaptureInfo, PaneContent: pane(command, elapsed)})
		if signal.State != StateBlocked || signal.Prompt == nil {
			t.Fatalf("no approval prompt: %+v", signal)
		}
		return signal.Prompt
	}
	first := infer("systemctl list-unit-files", "3s")
	for _, elapsed := range []string{"4s", "59s", "1m 05s", "12m 3s"} {
		if got := infer("systemctl list-unit-files", elapsed); got.ID != first.ID {
			t.Fatalf("identity moved with the Working clock (%s): %s != %s", elapsed, got.ID, first.ID)
		}
	}
	if other := infer("rm -rf /tmp/x", "3s"); other.ID == first.ID {
		t.Fatal("a different command kept the identity")
	}
}
