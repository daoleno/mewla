package classifier

import (
	"strings"
	"testing"
)

const subAgentPermissionPane = "❯ continue\n\n● Agent(run recon)\n  ⎿ Bash(cd x && sed -i s/a/b/ y)\n\n────────────────────\n Bash command · from the general-purpose agent\n Run shell command\n╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ cd /x && sed -i y\n │ Compound command contains cd with write operation - manual approval required\n │ to prevent path resolution bypass\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend · ctrl+x ctrl+k twice to stop background agents\n"

func TestClaudeSubAgentPermissionPromptIsStructured(t *testing.T) {
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: subAgentPermissionPane})
	if signal.State != StateBlocked || signal.Prompt == nil {
		t.Fatalf("not a structured blocked signal: %+v", signal)
	}
	prompt := signal.Prompt
	if prompt.Kind != PromptPermission || prompt.Provider != "claude" || prompt.ID == "" {
		t.Fatalf("identity: %+v", prompt)
	}
	if prompt.Title != "Bash command · from the general-purpose agent" {
		t.Fatalf("title %q", prompt.Title)
	}
	if len(prompt.Options) != 2 || prompt.Options[0].Number != 1 || prompt.Options[0].Label != "Yes" || !prompt.Options[0].Selected || prompt.Options[1].Label != "No" {
		t.Fatalf("options: %+v", prompt.Options)
	}
	for _, want := range []string{"Compound command contains cd with write operation", "Do you want to proceed?"} {
		if !strings.Contains(prompt.Request, want) {
			t.Fatalf("request lacks %q: %q", want, prompt.Request)
		}
	}
	again := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: subAgentPermissionPane + "\n"})
	if again.Prompt == nil || again.Prompt.ID != prompt.ID {
		t.Fatal("the same prompt changed identity")
	}
	other := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: replaceOnce(subAgentPermissionPane, "sed -i y", "rm -rf z")})
	if other.Prompt == nil || other.Prompt.ID == prompt.ID {
		t.Fatal("a different command kept the identity")
	}
}

func TestClaudePlanAndStartupPromptsAreStructured(t *testing.T) {
	plan := "Claude Code\n Here is the plan\n  1. do a\n  2. do b\n\nClaude has written up a plan and is ready to execute. Would you like to proceed?\n\n ❯ 1. Yes, clear context and auto-accept edits\n   2. Yes, auto-accept edits\n   3. Yes, manually approve edits\n   4. No, keep planning\n\n ctrl-g to edit plan\n"
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: plan})
	if signal.Prompt == nil || signal.Prompt.Kind != PromptPlan || len(signal.Prompt.Options) != 4 {
		t.Fatalf("plan: %+v", signal)
	}
	trust := " Accessing workspace:\n\n /home/u/proj\n\n Quick safety check: Is this a project you created or one you trust?\n\n ❯ 1. Yes, I trust this folder\n   2. No, exit\n\n Enter to confirm · Esc to cancel\n"
	signal = NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: trust})
	if signal.State != StateBlocked || signal.Prompt == nil || signal.Prompt.Kind != PromptStartup || signal.Prompt.Options[0].Label != "Yes, I trust this folder" {
		t.Fatalf("startup: %+v", signal)
	}
}

func TestClaudeProseAboveTheFooterIsNotAPrompt(t *testing.T) {
	pane := "Claude Code\n● I will ask:\n  1. Yes\n  2. No\n\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	if signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: pane}); signal.State == StateBlocked {
		t.Fatalf("prose became a prompt: %+v", signal)
	}
}

func replaceOnce(text, old, repl string) string { return strings.Replace(text, old, repl, 1) }

// A dialog Claude will refuse on its own carries a clock that ticks while the
// dialog stays open. The prompt a Worker's Brain was told about must still be
// that prompt on the next poll, or `worker answer` finds it "passed" while it
// is plainly open.
func TestPromptIdentityIgnoresCountdownsAndClocks(t *testing.T) {
	pane := func(countdown, yes string) string {
		return strings.Replace(strings.Replace(subAgentPermissionPane,
			" Do you want to proceed?\n", " "+countdown+"\n\n Do you want to proceed?\n", 1),
			"1. Yes\n", "1. Yes"+yes+"\n", 1)
	}
	infer := func(content string) *Prompt {
		t.Helper()
		signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: content})
		if signal.State != StateBlocked || signal.Prompt == nil {
			t.Fatalf("not a structured blocked signal for %q: %+v", content, signal)
		}
		return signal.Prompt
	}
	const deny = "⏱ Claude Code will automatically deny this request in %s, to avoid blocking progress on an unattended session"
	first := infer(pane(strings.Replace(deny, "%s", "about 28 seconds", 1), " (28s)"))
	for _, tick := range []struct{ countdown, yes string }{
		{strings.Replace(deny, "%s", "about 27 seconds", 1), " (27s)"},
		{strings.Replace(deny, "%s", "0:09", 1), " (1m 5s)"},
		{"", ""},
	} {
		got := infer(pane(tick.countdown, tick.yes))
		if got.ID != first.ID {
			t.Fatalf("the prompt changed identity as its clock ticked (%q %q): %s != %s", tick.countdown, tick.yes, got.ID, first.ID)
		}
	}
	if strings.Contains(first.Request, "automatically deny") || !strings.Contains(first.Request, "Compound command contains cd") {
		t.Fatalf("Brain is shown the clock, or lost the request: %q", first.Request)
	}
	// A different request is still a different prompt.
	other := infer(replaceOnce(pane(strings.Replace(deny, "%s", "about 28 seconds", 1), " (28s)"), "sed -i y", "rm -rf z"))
	if other.ID == first.ID {
		t.Fatal("a different command kept the identity")
	}
}

func TestStableTextDropsSpinnersAndBracketedClocks(t *testing.T) {
	for in, want := range map[string]string{
		"✻ Running tests… (12s)":       "Running tests…",
		"⠹ Waiting\n· Reading (1m 5s)": "Waiting\nReading",
		"Yes (28s)":                    "Yes",
		"sleep 30 && echo done":        "sleep 30 && echo done",
		"Keep the (2) copies":          "Keep the (2) copies",
	} {
		if got := stableText(in); got != want {
			t.Errorf("stableText(%q) = %q, want %q", in, got, want)
		}
	}
}
