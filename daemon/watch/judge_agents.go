package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Judgment runs on an agent the person actually has. It tries the Brain
// host's executor first, then the last one that worked, then the rest in a
// fixed order, skipping any that is not installed. Every agent runs the same
// way: a print-mode call in an empty directory with no tools, hooks, MCP
// servers or saved session, a prompt Redact has already cleaned, and a JSON
// answer checked against the same schema. What differs is only how each CLI
// is asked.
//
//	claude  claude -p, model sonnet, schema enforced by the CLI, cost reported
//	codex   codex exec, Codex's default model at low reasoning effort (the
//	        person's Codex config is not read), schema enforced by the CLI,
//	        tokens reported
//	pi      pi -p, the model set in the person's Pi, thinking off, schema in
//	        the prompt and checked here, tokens and cost reported
type judgeAgent struct {
	name string
	run  func(ctx context.Context, j *judge, path, system, prompt, schema string) (json.RawMessage, Usage, error)
}

var judgeAgents = []judgeAgent{
	{name: "claude", run: runClaude},
	{name: "codex", run: runCodex},
	{name: "pi", run: runPi},
}

// order lists the installed agents to try, preferred first.
func (j *judge) order() []judgeAgent {
	j.mu.Lock()
	good := j.lastGood
	j.mu.Unlock()
	wanted := []string{}
	if j.preferred != nil {
		wanted = append(wanted, strings.ToLower(strings.TrimSpace(j.preferred())))
	}
	wanted = append(wanted, good)
	for _, agent := range judgeAgents {
		wanted = append(wanted, agent.name)
	}
	var out []judgeAgent
	seen := map[string]bool{}
	for _, name := range wanted {
		for _, agent := range judgeAgents {
			if agent.name != name || seen[name] {
				continue
			}
			if _, err := j.lookPath(name); err != nil {
				continue
			}
			seen[name] = true
			out = append(out, agent)
		}
	}
	return out
}

// expected names the agent and model the next run would use first.
func (j *judge) expected() (string, string) {
	order := j.order()
	if len(order) == 0 {
		return "", ""
	}
	if order[0].name == "claude" {
		return "claude", judgeClaudeModel
	}
	return order[0].name, "default"
}

// runAgents asks each installed agent in turn until one answers.
func (j *judge) runAgents(ctx context.Context, system, prompt, schema string) (json.RawMessage, Usage, error) {
	order := j.order()
	if len(order) == 0 {
		return nil, Usage{}, errors.New("judgment needs Claude Code, Codex or Pi installed")
	}
	if err := os.MkdirAll(j.workdir, 0o700); err != nil {
		return nil, Usage{}, err
	}
	var failures []string
	for _, agent := range order {
		path, err := j.lookPath(agent.name)
		if err != nil {
			continue
		}
		output, usage, err := agent.run(ctx, j, path, system, prompt, schema)
		if err == nil {
			usage.Executor = agent.name
			j.mu.Lock()
			j.lastGood = agent.name
			j.mu.Unlock()
			return output, usage, nil
		}
		if ctx.Err() != nil {
			return nil, usage, ctx.Err()
		}
		failures = append(failures, agent.name+": "+err.Error())
	}
	return nil, Usage{}, errors.New(strings.Join(failures, "; "))
}

// execute runs a command in the judgment directory, bound to the daemon.
func (j *judge) command(ctx context.Context, path string, stdin string, args ...string) (stdout, stderr string, err error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = j.workdir
	command.Stdin = strings.NewReader(stdin)
	bindJudgeToDaemon(command)
	var out, errOut bytes.Buffer
	command.Stdout = &limitedWriter{buffer: &out, limit: judgeOutputLimit}
	command.Stderr = &limitedWriter{buffer: &errOut, limit: 64 << 10}
	err = command.Run()
	return out.String(), errOut.String(), err
}

// claudeResult is the part of claude -p --output-format json we read.
type claudeResult struct {
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	DurationMS       int             `json:"duration_ms"`
	TerminalReason   string          `json:"terminal_reason"`
	Usage            struct {
		InputTokens         int `json:"input_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
		OutputTokens        int `json:"output_tokens"`
	} `json:"usage"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
}

// runClaude uses Claude Code in print mode. Only the user's own settings
// load, because they carry how this person signs in.
func runClaude(ctx context.Context, j *judge, path, system, prompt, schema string) (json.RawMessage, Usage, error) {
	stdout, stderr, runErr := j.command(ctx, path, prompt, "-p",
		"--model", judgeClaudeModel,
		"--tools", "",
		"--no-session-persistence",
		"--setting-sources", "user",
		"--settings", `{"disableAllHooks":true}`,
		"--strict-mcp-config",
		"--output-format", "json",
		"--json-schema", schema,
		"--system-prompt", system,
		"--max-budget-usd", judgeBudgetUSD,
	)
	var result claudeResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		if runErr != nil {
			return nil, Usage{}, fmt.Errorf("exited: %v: %s", runErr, excerpt(stderr, 300))
		}
		return nil, Usage{}, fmt.Errorf("output is not JSON: %s", excerpt(stdout, 300))
	}
	usage := Usage{
		InputTokens: result.Usage.InputTokens, CacheCreationTokens: result.Usage.CacheCreationTokens,
		CacheReadTokens: result.Usage.CacheReadTokens, OutputTokens: result.Usage.OutputTokens,
		CostUSD: result.TotalCostUSD, DurationMS: result.DurationMS,
	}
	models := make([]string, 0, len(result.ModelUsage))
	for model := range result.ModelUsage {
		models = append(models, model)
	}
	sort.Strings(models)
	usage.Models = strings.Join(models, ",")
	if result.IsError || len(result.StructuredOutput) == 0 || string(result.StructuredOutput) == "null" {
		return nil, usage, fmt.Errorf("no structured output (%s): %s", result.TerminalReason, excerpt(result.Result, 300))
	}
	return result.StructuredOutput, usage, nil
}

// runCodex uses codex exec with the person's own sign-in but not their
// config, so no hooks, MCP servers or custom instructions load. It runs
// read-only in an empty directory and keeps no session.
func runCodex(ctx context.Context, j *judge, path, system, prompt, schema string) (json.RawMessage, Usage, error) {
	dir, err := os.MkdirTemp(j.workdir, "codex-")
	if err != nil {
		return nil, Usage{}, err
	}
	defer os.RemoveAll(dir)
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		return nil, Usage{}, err
	}
	lastPath := filepath.Join(dir, "last.txt")
	// Codex has no system prompt flag: the playbook leads the prompt.
	stdout, stderr, runErr := j.command(ctx, path, "# Instructions\n\n"+system+"\n\n# Input\n\n"+prompt,
		"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check",
		"-s", "read-only", "-c", `model_reasoning_effort="low"`,
		"--output-schema", schemaPath, "--json", "-o", lastPath, "-")
	var usage Usage
	for _, line := range strings.Split(stdout, "\n") {
		var event struct {
			Type  string `json:"type"`
			Usage struct {
				Input       int `json:"input_tokens"`
				Cached      int `json:"cached_input_tokens"`
				CacheWrite  int `json:"cache_write_input_tokens"`
				Output      int `json:"output_tokens"`
				ReasonedOut int `json:"reasoning_output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "turn.completed" {
			usage.InputTokens += event.Usage.Input - event.Usage.Cached
			usage.CacheReadTokens += event.Usage.Cached
			usage.CacheCreationTokens += event.Usage.CacheWrite
			usage.OutputTokens += event.Usage.Output
		}
	}
	usage.Models = "default"
	last, readErr := os.ReadFile(lastPath)
	if runErr != nil || readErr != nil || len(bytes.TrimSpace(last)) == 0 {
		if runErr == nil {
			runErr = errors.New("no final message")
		}
		return nil, usage, fmt.Errorf("exited: %v: %s", runErr, excerpt(stderr+" "+stdout, 300))
	}
	output, err := jsonObject(string(last))
	return output, usage, err
}

// runPi uses pi in print mode with every extension, MCP server, skill and
// context file turned off. Pi has no schema flag, so the schema rides in the
// system prompt and the answer is checked when it is applied.
func runPi(ctx context.Context, j *judge, path, system, prompt, schema string) (json.RawMessage, Usage, error) {
	system += "\n\nReply with only one JSON object that matches this JSON Schema, with no prose and no code fence:\n" + schema
	stdout, stderr, runErr := j.command(ctx, path, prompt, "-p", "--mode", "json",
		"--no-tools", "--no-session", "--no-extensions", "--no-mcp", "--no-skills",
		"--no-context-files", "--no-prompt-templates", "--thinking", "off",
		"--system-prompt", system)
	var text string
	var usage Usage
	for _, line := range strings.Split(stdout, "\n") {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Model   string `json:"model"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Usage struct {
					Input      int `json:"input"`
					Output     int `json:"output"`
					CacheRead  int `json:"cacheRead"`
					CacheWrite int `json:"cacheWrite"`
					Cost       struct {
						Total float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
				Duration int `json:"durationMs"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "turn_end" || event.Message.Role != "assistant" {
			continue
		}
		text = ""
		for _, part := range event.Message.Content {
			if part.Type == "text" {
				text += part.Text
			}
		}
		u := event.Message.Usage
		usage.InputTokens += u.Input
		usage.OutputTokens += u.Output
		usage.CacheReadTokens += u.CacheRead
		usage.CacheCreationTokens += u.CacheWrite
		usage.CostUSD += u.Cost.Total
		usage.DurationMS += event.Message.Duration
		usage.Models = event.Message.Model
	}
	if strings.TrimSpace(text) == "" {
		if runErr == nil {
			runErr = errors.New("no final message")
		}
		return nil, usage, fmt.Errorf("exited: %v: %s", runErr, excerpt(stderr+" "+stdout, 300))
	}
	output, err := jsonObject(text)
	return output, usage, err
}

// jsonObject extracts the one JSON object from an agent's final message,
// which may arrive in a code fence.
func jsonObject(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("the answer is not a JSON object: %s", excerpt(text, 200))
	}
	raw := json.RawMessage(text[start : end+1])
	if !json.Valid(raw) {
		return nil, fmt.Errorf("the answer is not valid JSON: %s", excerpt(text, 200))
	}
	return raw, nil
}
