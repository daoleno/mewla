package work

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func TestParseCursorModelsReadsItsList(t *testing.T) {
	// The shape of cursor-agent's ./src/commands/models.ts output (2026.10.22), with its colours.
	out := "\x1b[2mAvailable models\x1b[22m\n\n\x1b[32mgpt-5\x1b[39m \x1b[2m- GPT-5\x1b[22m\x1b[2m (current)\x1b[22m\n" +
		"\x1b[36msonnet-4-thinking\x1b[39m \x1b[2m- Sonnet 4 Thinking\x1b[22m\x1b[2m (default)\x1b[22m\n\x1b[36mauto\x1b[39m\n\n" +
		"\x1b[2mTip: use --model <id> (or /model <id> in interactive mode) to switch.\x1b[22m\n"
	want := []CLIModel{{ID: "gpt-5", Name: "GPT-5", Current: true}, {ID: "sonnet-4-thinking", Name: "Sonnet 4 Thinking", Default: true}, {ID: "auto"}}
	if got := ParseCursorModels(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v", got)
	}
	if got := ParseCursorModels("Error: Authentication required. Run 'agent login'\n"); len(got) != 0 {
		t.Fatalf("a refusal was read as models: %#v", got)
	}
}

func TestParseGrokModelsReadsItsList(t *testing.T) {
	// grok 1.0.77 `grok models`, signed out.
	out := "You are not authenticated.\n\nDefault model: grok-4.6\n\nAvailable models:\n  * grok-4.6 (default)\n  - grok-4.5\n"
	want := []CLIModel{{ID: "grok-4.6", Default: true}, {ID: "grok-4.5"}}
	if got := ParseGrokModels(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v", got)
	}
}

func TestLaunchModelFlag(t *testing.T) {
	for command, want := range map[string]string{
		"grok --no-alt-screen -m grok-4.5":   "grok-4.5",
		"cursor-agent --model=gpt-5 --force": "gpt-5",
		"cursor-agent --force":               "",
	} {
		if got := LaunchModelFlag(command); got != want {
			t.Errorf("%q: %q", command, got)
		}
	}
}

// Signed out, Grok still prints its models; none can run, so it has no menu
// (11-24: the Session offered grok-4.5 and typed /model into the sign-in
// screen, reporting the switch as made).
func TestReadCLIModelsRefusesSignedOutGrok(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/grok"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'You are not authenticated.\\n\\nDefault model: grok-4.6\\n\\nAvailable models:\\n  * grok-4.6 (default)\\n  - grok-4.5\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if models, err := ReadCLIModels(context.Background(), WorkerProviderGrok, script, dir); err == nil || len(models) != 0 {
		t.Fatalf("signed-out Grok listed %v, err %v", models, err)
	}
}

// The command a restarted daemon reads off an OpenCode process names its session.
func TestOpenCodeSessionReadOffItsProcess(t *testing.T) {
	token, present, err := ProviderResumeToken(WorkerProviderOpenCode, "opencode --session ses_df3d77817ffeKfRAo26OMA6s7w")
	if err != nil || !present || token != "ses_df3d77817ffeKfRAo26OMA6s7w" {
		t.Fatalf("session %q present %v err %v", token, present, err)
	}
}
