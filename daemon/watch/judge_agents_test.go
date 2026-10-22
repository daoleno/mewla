package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
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
const piOK = "cat >/dev/null\n" + `printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"flash","content":[{"type":"text","text":"` + "```json\\n" + `{\"from\":\"pi\"}` + "\\n```" + `"}],"usage":{"input":10,"output":4,"cost":{"total":0.001}}}}'`

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

func routedFixture(t *testing.T, installed map[string]string, routing string) (*judge, string) {
	t.Helper()
	j := agentFixture(t, installed, "")
	path := filepath.Join(t.TempDir(), "routing.md")
	if err := os.WriteFile(path, []byte(routing), 0o600); err != nil {
		t.Fatal(err)
	}
	j.routingPath = path
	return j, path
}

const routingTable = "# Worker Routing\n\n| Task | Executor | Model | Reasoning |\n| --- | --- | --- | --- |\n| Frontend | claude | claude-sonnet-5-5 | high |\n"

func TestRoutingRowPicksTheJudgmentExecutorModelAndReasoning(t *testing.T) {
	installed := map[string]string{"claude": claudeOK, "pi": piOK, "codex": codexOK}
	j, _ := routedFixture(t, installed, routingTable+"| Watch judgment, reading signals | pi | opencode-go/deepseek-v4.1-flash | low |\n")
	route, routing := j.routed()
	if route != (judgeRoute{Executor: "pi", Model: "opencode-go/deepseek-v4.1-flash", Effort: "low"}) || routing.Source != "routing.md" || routing.Reason != "" {
		t.Fatalf("route = %#v routing = %#v", route, routing)
	}
	// Only the routed executor is asked, even when it would fail.
	if order := j.order(); len(order) != 1 || order[0].name != "pi" {
		t.Fatalf("order = %v", order)
	}
	if name, model := j.expected(); name != "pi" || model != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("expected = %s %s", name, model)
	}
	out, usage, err := j.runAgents(context.Background(), "s", "p", "{}")
	if err != nil || !strings.Contains(string(out), "pi") || usage.Executor != "pi" {
		t.Fatalf("out=%s usage=%#v err=%v", out, usage, err)
	}
}

func TestRoutingRowArgumentsReachTheCLI(t *testing.T) {
	record := `echo "$@" > "$RECORD"; `
	t.Setenv("RECORD", filepath.Join(t.TempDir(), "args"))
	cases := map[string]struct{ row, script, want string }{
		"claude": {"| Watch judgment | claude | opus | max |", record + claudeOK, "--model opus"},
		"codex":  {"| Watch judgment | codex | gpt-x | high |", record + codexOK, `-m gpt-x`},
		"pi":     {"| Watch judgment | pi | prov/m | medium |", record + piOK, "--model prov/m"},
	}
	for name, c := range cases {
		j, _ := routedFixture(t, map[string]string{name: c.script}, routingTable+c.row+"\n")
		if _, _, err := j.runAgents(context.Background(), "s", "p", "{}"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := os.ReadFile(os.Getenv("RECORD"))
		args := string(raw)
		effort := map[string]string{"claude": "--effort max", "codex": `model_reasoning_effort="high"`, "pi": "--thinking medium"}[name]
		if !strings.Contains(args, c.want) || !strings.Contains(args, effort) {
			t.Errorf("%s args = %s", name, args)
		}
		// The guarantees are not the row's to change.
		for _, kept := range map[string][]string{
			"claude": {"--tools ", "--no-session-persistence", "--strict-mcp-config", "--max-budget-usd"},
			"codex":  {"--ephemeral", "--ignore-user-config", "read-only"},
			"pi":     {"--no-tools", "--no-session", "--no-mcp", "--no-extensions"},
		}[name] {
			if !strings.Contains(args, kept) {
				t.Errorf("%s lost %s: %s", name, kept, args)
			}
		}
	}
}

func TestRoutingFallsBackAndSaysWhy(t *testing.T) {
	installed := map[string]string{"claude": claudeOK}
	for name, c := range map[string]struct{ routing, reason string }{
		"missing row":       {routingTable, "no Watch judgment row"},
		"not installed":     {routingTable + "| Watch judgment | pi | m | low |\n", `"pi" is not installed`},
		"cannot judge":      {routingTable + "| Watch judgment | opencode | m | low |\n", "cannot judge"},
		"bad reasoning":     {routingTable + "| Watch judgment | claude | sonnet | off |\n", `reasoning "off"`},
		"wrong cell count":  {routingTable + "| Watch judgment | claude | sonnet |\n", "four cells"},
		"model with spaces": {routingTable + "| Watch judgment | claude | some model | low |\n", "not a model id"},
		"model like a flag": {routingTable + "| Watch judgment | claude | --dangerous | low |\n", "not a model id"},
	} {
		j, _ := routedFixture(t, installed, c.routing)
		route, routing := j.routed()
		if route.Executor != "" || routing.Source != "automatic" || !strings.Contains(routing.Reason, c.reason) {
			t.Errorf("%s: route=%#v routing=%#v", name, route, routing)
		}
		// The automatic choice still judges.
		if _, usage, err := j.runAgents(context.Background(), "s", "p", "{}"); err != nil || usage.Executor != "claude" {
			t.Errorf("%s: fallback failed: %v %#v", name, err, usage)
		}
	}
	// No routing.md at all, or no path.
	j := agentFixture(t, installed, "")
	if _, routing := j.routed(); routing.Source != "automatic" || routing.Reason != "" {
		t.Errorf("no path: %#v", routing)
	}
	j.routingPath = filepath.Join(t.TempDir(), "absent.md")
	if _, routing := j.routed(); routing.Source != "automatic" || !strings.Contains(routing.Reason, "not readable") {
		t.Errorf("absent file: %#v", routing)
	}
}

func TestRoutingIsReadAgainWhenTheFileChanges(t *testing.T) {
	installed := map[string]string{"claude": claudeOK, "pi": piOK}
	j, path := routedFixture(t, installed, routingTable+"| Watch judgment | claude | sonnet | low |\n")
	if route, _ := j.routed(); route.Executor != "claude" {
		t.Fatalf("first read = %#v", route)
	}
	later := time.Now().Add(time.Minute)
	write := func(text string) {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(path, later, later)
		later = later.Add(time.Minute)
	}
	write(routingTable + "| Watch judgment | pi | m/x | off |\n")
	if route, routing := j.routed(); route.Executor != "pi" || routing.Source != "routing.md" {
		t.Fatalf("after edit = %#v %#v", route, routing)
	}
	write(routingTable + "| Watch judgment | nope | m | off |\n")
	if route, routing := j.routed(); route.Executor != "" || routing.Source != "automatic" || routing.Reason == "" {
		t.Fatalf("after breaking it = %#v %#v", route, routing)
	}
	write(routingTable + "| watch judgment | claude | opus | high |\n")
	if route, _ := j.routed(); route != (judgeRoute{Executor: "claude", Model: "opus", Effort: "high"}) {
		t.Fatalf("after fixing it = %#v", route)
	}
	if status := j.status(); status.Routing.Source != "routing.md" || status.Executor != "claude" || status.Model != "opus" {
		t.Fatalf("status = %#v", status)
	}
}

func TestDefaultRoutingGuideCarriesAValidWatchJudgmentRow(t *testing.T) {
	route, found, err := parseRoutingRow(brain.DefaultRoutingGuide(), func(string) bool { return true })
	if err != nil || !found || route.Executor != "claude" || route.Model != "sonnet" {
		t.Fatalf("default row = %#v found=%v err=%v", route, found, err)
	}
}
