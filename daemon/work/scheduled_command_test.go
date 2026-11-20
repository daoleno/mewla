package work

import (
	"errors"
	"testing"
)

func scheduledCommandConfig() *ExecutorConfig {
	return NewExecutorConfig(map[string]Executor{
		"claude":   {Name: "claude", Command: "claude --configured"},
		"codex":    {Name: "codex", Command: "codex"},
		"pi":       {Name: "pi", Command: "pi"},
		"opencode": {Name: "opencode", Command: "opencode"},
	})
}

func TestResolveScheduledActionCommandAppliesModelAndReasoningLikeASpawn(t *testing.T) {
	execs := scheduledCommandConfig()
	for _, test := range []struct {
		executor, model, reasoning, want string
	}{
		{"claude", "", "", "claude --configured --permission-mode bypassPermissions"},
		{"claude", "claude-haiku", "low", "claude --configured --model claude-haiku --effort low --permission-mode bypassPermissions"},
		{"codex", "gpt-x", "high", "codex --model gpt-x -c 'model_reasoning_effort=\"high\"' --dangerously-bypass-approvals-and-sandbox"},
	} {
		got, err := execs.ResolveScheduledActionCommand(test.executor, test.model, test.reasoning)
		if err != nil {
			t.Fatalf("%+v: %v", test, err)
		}
		if got != test.want {
			t.Fatalf("%s %q %q = %q, want %q", test.executor, test.model, test.reasoning, got, test.want)
		}
	}
}

// The errors are the ones ResolveWorkerCommand gives for the same choice.
func TestResolveScheduledActionCommandRefusesWhatASpawnRefuses(t *testing.T) {
	execs := scheduledCommandConfig()
	for _, test := range []struct{ executor, model, reasoning string }{
		{"nope", "", ""},
		{"", "m", ""},
		{"claude", "", "turbo"},
		{"codex", "", "turbo"},
		{"opencode", "m", ""},
		{"opencode", "", "low"},
		{"claude", "bad\nmodel", ""},
	} {
		_, spawn := execs.ResolveWorkerCommand(test.executor, "", test.model, test.reasoning, true)
		_, scheduled := execs.ResolveScheduledActionCommand(test.executor, test.model, test.reasoning)
		if spawn == nil || scheduled == nil {
			t.Fatalf("%+v: spawn %v, scheduled %v; both should refuse", test, spawn, scheduled)
		}
		if spawn.Error() != scheduled.Error() && !errors.Is(scheduled, ErrUnknownExecutor) {
			t.Fatalf("%+v: spawn says %q, scheduled says %q", test, spawn, scheduled)
		}
	}
}
