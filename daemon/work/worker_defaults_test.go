package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerDefaultsAtomicLiveAndBoundaries(t *testing.T) {
	t.Setenv("ZEN_DELEGATED_EXECUTOR", "")
	path := filepath.Join(t.TempDir(), "executors.toml")
	original := "# retained comment\ndelegated_executor = \"pi\"\n\n[[executors]]\nname = \"codex\"\ncommand = \"codex --model gpt-6-sol\"\n\n[[executors]]\nname = \"pi\"\ncommand = \"pi\"\n"
	writeExecutorsTOML(t, path, original)
	c := loadExecutorsOrFatal(t, path)
	d, err := c.SetWorkerDefaults(WorkerDefaults{Executor: "codex", Model: "gpt-6-astra", Reasoning: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Command, "--model gpt-6-astra") || !strings.Contains(d.Command, `model_reasoning_effort="medium"`) || !strings.Contains(d.Command, CodexFullAuthorizationFlag) || strings.Contains(d.Command, "gpt-6-sol") {
		t.Fatal(d)
	}
	got, err := c.ResolveWorkerCommand("", "", "", "", true)
	if err != nil || got != d.Command {
		t.Fatalf("%q %v", got, err)
	}
	reloaded := loadExecutorsOrFatal(t, path)
	rd, err := reloaded.WorkerDefaults()
	if err != nil || rd != d {
		t.Fatalf("%+v %v", rd, err)
	}
	manual, err := c.ResolveWorkerCommand("codex", "", "", "", false)
	if err != nil || manual != "codex --model gpt-6-sol" {
		t.Fatalf("manual changed: %q %v", manual, err)
	}
	if c.ByName["codex"].Command != manual {
		t.Fatal("host catalog mutated")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), original[strings.Index(original, "[[executors]]"):]) || !strings.Contains(string(raw), "# retained comment") {
		t.Fatal("unrelated settings lost")
	}
	if _, err := c.SetWorkerDefaults(WorkerDefaults{Executor: "codex", Model: "x", Reasoning: "invalid"}); err == nil {
		t.Fatal("expected invalid effort")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("invalid save changed file")
	}
	// Force a real persistence error after validation; live state must remain.
	c.path = filepath.Join(path, "not-a-directory")
	if _, err := c.SetWorkerDefaults(WorkerDefaults{Executor: "codex", Model: "different", Reasoning: "high"}); err == nil {
		t.Fatal("expected failed save")
	}
	afterDefaults, _ := c.WorkerDefaults()
	if afterDefaults != d {
		t.Fatalf("half update: %+v", afterDefaults)
	}
}

func TestWorkerDefaultCommandResumePrecedence(t *testing.T) {
	c := NewExecutorConfig("codex", map[string]Executor{"codex": {Command: "codex --model old"}, "pi": {Command: "pi"}})
	_, err := c.SetWorkerDefaults(WorkerDefaults{Executor: "codex", Model: "gpt-6-astra", Reasoning: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ command, model, effort, wantModel, wantEffort string }{
		{"codex resume thread-id", "", "", "gpt-6-astra", "medium"},
		{"codex resume thread-id --model custom -c 'model_reasoning_effort=\"high\"'", "", "", "custom", "high"},
		{"codex resume thread-id --model custom", "override", "low", "override", "low"},
		{"env NOTE=--dangerously-bypass-approvals-and-sandbox codex", "", "", "gpt-6-astra", "medium"},
	} {
		cmd, err := c.ResolveWorkerCommand("", tc.command, tc.model, tc.effort, true)
		if err != nil {
			t.Fatal(err)
		}
		args, ok := splitSupportedLaunchFields(cmd)
		if !ok {
			t.Fatal(cmd)
		}
		n := 0
		for _, a := range args {
			if a == CodexFullAuthorizationFlag {
				n++
			}
		}
		if n != 1 {
			t.Fatal(cmd)
		}
		_, m, e, err := codexCommandParts(cmd, true)
		if err != nil || m != tc.wantModel || e != tc.wantEffort || strings.Count(cmd, "--model ") != 1 {
			t.Fatalf("%q %q %q %v", cmd, m, e, err)
		}
	}
	for _, cmd := range []string{"codex resume id -a on-request", "codex -s read-only", "codex --full-auto", "codex -c 'approval_policy=\"on-request\"'", "codex --model a --model b", "codex; echo bypass", "codex -aon-request"} {
		if _, err := c.ResolveWorkerCommand("", cmd, "", "", true); err == nil {
			t.Fatalf("accepted conflict: %s", cmd)
		}
	}
}

func TestDelegatedClaudeAutoIsNotUnattended(t *testing.T) {
	for _, command := range []string{"claude --permission-mode auto", "claude --permission-mode dontAsk", "claude --permission-mode acceptEdits", "claude --permission-mode default"} {
		if _, err := PrepareDelegatedCommand(WorkerProviderClaude, command); err == nil {
			t.Fatalf("accepted interactive policy %q", command)
		}
	}
	for _, command := range []string{"claude", "claude --permission-mode bypassPermissions", "claude --dangerously-skip-permissions"} {
		got, err := PrepareDelegatedCommand(WorkerProviderClaude, command)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "bypassPermissions") && !strings.Contains(got, "--dangerously-skip-permissions") {
			t.Fatal(got)
		}
	}
	if got := HardenClaudeCommand("claude --permission-mode auto"); got != "claude --permission-mode auto" {
		t.Fatal("host policy changed")
	}
}
