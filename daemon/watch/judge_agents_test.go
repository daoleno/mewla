package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeCLI(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func agentFixture(t *testing.T, installed map[string]string, preferred string) *judge {
	t.Helper()
	bin := t.TempDir()
	for name, script := range installed {
		fakeCLI(t, bin, name, script)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := newJudge(store, t.TempDir())
	j.lookPath = func(name string) (string, error) {
		path := filepath.Join(bin, name)
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	j.preferred = func() string { return preferred }
	return j
}

const claudeOK = `echo '{"is_error":false,"structured_output":{"from":"claude"},"total_cost_usd":0.02,"usage":{"input_tokens":5,"output_tokens":7},"modelUsage":{"claude-sonnet-5-5":{}}}'`
const claudeBroken = `echo "not signed in" >&2; exit 1`
const codexOK = `out=""; while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
cat >/dev/null
echo '{"from":"codex"}' > "$out"
echo '{"type":"turn.completed","usage":{"input_tokens":1000,"cached_input_tokens":400,"output_tokens":30}}'`
const piOK = "cat >/dev/null\n" + `echo '{"type":"turn_end","message":{"role":"assistant","model":"flash","content":[{"type":"text","text":"` + "```json\\n" + `{\"from\":\"pi\"}` + "\\n```" + `"}],"usage":{"input":10,"output":4,"cost":{"total":0.001}}}}'`

func TestJudgmentFallsBackAcrossInstalledAgents(t *testing.T) {
	j := agentFixture(t, map[string]string{"claude": claudeBroken, "codex": codexOK, "pi": piOK}, "")
	out, usage, err := j.runAgents(context.Background(), "system", "prompt", "{}")
	if err != nil || !strings.Contains(string(out), "codex") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if usage.Executor != "codex" || usage.InputTokens != 600 || usage.CacheReadTokens != 400 || usage.OutputTokens != 30 {
		t.Fatalf("usage = %#v", usage)
	}
	// The one that worked is tried first next time.
	if order := j.order(); order[0].name != "codex" {
		t.Fatalf("order = %v", order)
	}
}

func TestJudgmentPrefersTheBrainHostsExecutor(t *testing.T) {
	j := agentFixture(t, map[string]string{"claude": claudeOK, "pi": piOK}, "pi")
	out, usage, err := j.runAgents(context.Background(), "s", "p", "{}")
	if err != nil || !strings.Contains(string(out), "pi") || usage.Executor != "pi" || usage.CostUSD != 0.001 || usage.Models != "flash" {
		t.Fatalf("out=%s usage=%#v err=%v", out, usage, err)
	}
	if name, model := j.expected(); name != "pi" || model != "default" {
		t.Fatalf("expected = %s %s", name, model)
	}
}

func TestClaudeKeepsItsSonnetModelAndCost(t *testing.T) {
	j := agentFixture(t, map[string]string{"claude": claudeOK}, "claude")
	out, usage, err := j.runAgents(context.Background(), "s", "p", "{}")
	if err != nil || !strings.Contains(string(out), "claude") || usage.CostUSD != 0.02 || usage.Models != "claude-sonnet-5-5" {
		t.Fatalf("out=%s usage=%#v err=%v", out, usage, err)
	}
	if name, model := j.expected(); name != "claude" || model != "sonnet" {
		t.Fatalf("expected = %s %s", name, model)
	}
}

func TestJudgmentReportsEveryFailureAndMissingAgents(t *testing.T) {
	j := agentFixture(t, map[string]string{"claude": claudeBroken, "pi": "echo oops >&2; exit 2"}, "")
	_, _, err := j.runAgents(context.Background(), "s", "p", "{}")
	if err == nil || !strings.Contains(err.Error(), "claude:") || !strings.Contains(err.Error(), "pi:") {
		t.Fatalf("err = %v", err)
	}
	none := agentFixture(t, nil, "")
	if _, _, err := none.runAgents(context.Background(), "s", "p", "{}"); err == nil || !strings.Contains(err.Error(), "installed") {
		t.Fatalf("err = %v", err)
	}
}
