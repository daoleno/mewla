package work

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveWorkerCommandAppliesPerLaunchModelAndReasoning(t *testing.T) {
	cfg := NewExecutorConfig(map[string]Executor{
		"codex":    {Name: "codex", Command: "codex"},
		"claude":   {Name: "claude", Command: "claude --model sonnet --effort=low"},
		"pi":       {Name: "pi", Command: "pi --thinking low", Kind: "pi"},
		"grok":     {Name: "grok", Command: "grok -m old"},
		"opencode": {Name: "opencode", Command: "opencode", Kind: "opencode"},
	})
	for _, tc := range []struct {
		executor, model, reasoning string
		want                       []string
		absent                     []string
	}{
		{"codex", "gpt-6-astra", "high", []string{"codex --model gpt-6-astra -c 'model_reasoning_effort=\"high\"'", CodexFullAuthorizationFlag}, nil},
		{"claude", "claude-opus-5-5", "high", []string{"claude --model claude-opus-5-5 --effort high", ClaudeFullAuthorizationFlag}, []string{"sonnet", "low"}},
		{"pi", "opencode-go/deepseek-v4.1-flash", "high", []string{"pi --model opencode-go/deepseek-v4.1-flash --thinking high", "--session /"}, []string{"low"}},
		{"grok", "grok-5", "", []string{"grok --model grok-5"}, []string{"old"}},
		{"opencode", "openai/gpt-6", "", []string{"opencode --model openai/gpt-6 --auto"}, nil},
		{"claude", "", "", []string{"claude --model sonnet --effort=low"}, nil},
	} {
		got, err := cfg.ResolveWorkerCommand(tc.executor, "", tc.model, tc.reasoning, true)
		if err != nil {
			t.Fatalf("%s: %v", tc.executor, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s command = %q, want %q", tc.executor, got, want)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(got, absent) {
				t.Fatalf("%s command = %q, must not keep %q", tc.executor, got, absent)
			}
		}
	}
}

func TestResolveWorkerCommandRejectsUnsupportedSelection(t *testing.T) {
	cfg := NewExecutorConfig(map[string]Executor{
		"claude": {Name: "claude", Command: "claude"},
		"pi":     {Name: "pi", Command: "pi", Kind: "pi"},
		"grok":   {Name: "grok", Command: "grok"},
		"dsh":    {Name: "dsh", Command: "dsh", Kind: "dsh"},
	})
	for _, tc := range []struct{ executor, model, reasoning, want string }{
		{"claude", "", "ultra", "accepted: low, medium, high, xhigh, max"},
		{"pi", "", "extreme", "accepted: off, minimal"},
		{"pi", "sonnet:high", "low", "already carries a thinking level"},
		{"grok", "", "high", "no per-launch reasoning option"},
		{"dsh", "x", "", "no per-launch model/reasoning option"},
	} {
		_, err := cfg.ResolveWorkerCommand(tc.executor, "", tc.model, tc.reasoning, true)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s %q/%q err = %v, want %q", tc.executor, tc.model, tc.reasoning, err, tc.want)
		}
	}
	if _, err := cfg.ResolveWorkerCommand("", "", "", "", true); !errors.Is(err, ErrUnknownExecutor) {
		t.Fatalf("empty executor err = %v, want ErrUnknownExecutor", err)
	}
}
