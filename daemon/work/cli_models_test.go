package work

import (
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
