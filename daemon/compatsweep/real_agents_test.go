//go:build linux || darwin

package compatsweep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/google/uuid"
)

// realAgent reports whether MEWLA_SWEEP_REAL names the agent: those
// scenarios use a real CLI and cost model calls.
func realAgent(t *testing.T, id string) {
	t.Helper()
	for _, name := range strings.Split(os.Getenv("MEWLA_SWEEP_REAL"), ",") {
		if strings.TrimSpace(name) == id {
			return
		}
	}
	t.Skipf("set MEWLA_SWEEP_REAL to include %s", id)
}

type realOptions struct {
	// fresh leaves the agents' own first-run state empty (no onboarding,
	// theme or API-key answer recorded); sign-ins are still copied.
	fresh bool
	// signedOut copies no sign-in and passes no gateway key: the agent is
	// installed but nobody ever logged in.
	signedOut bool
	// executor locks Brain's agent.
	executor string
}

// realExecutors are the agents' commands, on models this machine's gateway
// serves.
const realExecutors = `[[executors]]
name = "claude"
command = "claude --model claude-sonnet-5-5"

[[executors]]
name = "codex"
command = "codex --model gpt-6-sol"
`

// newRealEnv is a fresh computer with the real claude and codex on PATH and
// copies of this machine's sign-ins in its HOME; the person's own ~/.claude
// and ~/.codex are only read.
func newRealEnv(t *testing.T, opts realOptions) *env {
	t.Helper()
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude is not installed here")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed here")
	}
	e := newEnv(t, envOptions{
		executors:    realExecutors,
		path:         []string{filepath.Dir(codex), "/usr/local/bin", "/usr/bin", "/bin"},
		gateway:      !opts.signedOut,
		hostExecutor: opts.executor,
	})
	if target, err := filepath.EvalSymlinks(claude); err == nil {
		claude = target
	}
	if err := os.Symlink(claude, filepath.Join(e.tools, "claude")); err != nil {
		t.Fatal(err)
	}
	realHome, _ := os.UserHomeDir()
	if !opts.signedOut {
		copyFile(t, filepath.Join(realHome, ".claude", ".credentials.json"), filepath.Join(e.home, ".claude", ".credentials.json"))
		copyFile(t, filepath.Join(realHome, ".codex", "auth.json"), filepath.Join(e.home, ".codex", "auth.json"))
	}
	if !opts.signedOut {
		// This machine's provider block reaches its gateway without a
		// sign-in; a signed-out computer has OpenAI's default, which needs one.
		writeFile(t, filepath.Join(e.home, ".codex", "config.toml"), minimalCodexConfig(t, filepath.Join(realHome, ".codex", "config.toml")), 0o600)
	}
	if !opts.fresh {
		key := os.Getenv("ANTHROPIC_API_KEY")
		approved := []string{}
		if len(key) >= 20 {
			approved = append(approved, key[len(key)-20:])
		}
		raw, _ := json.Marshal(map[string]any{
			"hasCompletedOnboarding": true, "theme": "dark",
			"customApiKeyResponses": map[string]any{"approved": approved, "rejected": []string{}},
		})
		writeFile(t, filepath.Join(e.home, ".claude.json"), string(raw), 0o600)
	}
	return e
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Skipf("no sign-in to copy: %v", err)
	}
	writeFile(t, to, string(raw), 0o600)
}

// minimalCodexConfig keeps only what reaches the model from the person's
// Codex config: the model keys and the provider they use.
func minimalCodexConfig(t *testing.T, path string) string {
	t.Helper()
	var config map[string]any
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return ""
	}
	out := map[string]any{}
	for _, key := range []string{"model", "model_provider", "model_reasoning_effort"} {
		if value, ok := config[key]; ok {
			out[key] = value
		}
	}
	if provider, _ := config["model_provider"].(string); provider != "" {
		if providers, ok := config["model_providers"].(map[string]any); ok {
			if block, ok := providers[provider]; ok {
				out["model_providers"] = map[string]any{provider: block}
			}
		}
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// find returns the first value under key anywhere in a decoded message.
func find(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		if found, ok := typed[key]; ok {
			return found
		}
		for _, child := range typed {
			if found := find(child, key); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := find(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}

// brainHost waits for a snapshot naming a running Brain host on executor and
// returns its Session id.
func brainHost(c *client, executor string, timeout time.Duration) (string, map[string]any) {
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		requestID := "snap-" + uuid.NewString()[:8]
		c.send(map[string]any{"type": "brain_snapshot", "request_id": requestID})
		message, ok := c.waitFor(5*time.Second, func(m map[string]any) bool {
			return str(m["type"]) == "brain_snapshot" && str(m["request_id"]) == requestID
		})
		if ok {
			last = message
			host, _ := find(message, "host_worker").(map[string]any)
			hostExecutor, _ := find(message, "host_executor").(map[string]any)
			if host != nil && str(host["id"]) != "" && find(message, "host_problem") == nil &&
				(executor == "" || str(hostExecutor["id"]) == executor) {
				return str(host["id"]), message
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", last
}

// chat sends text to Brain as the web Chat does and waits for the computer to
// take it.
func chat(c *client, hostID, text string) (string, error) {
	requestID := "sweep-" + uuid.NewString()[:8]
	c.send(map[string]any{"type": "send_input", "request_id": requestID, "worker_id": hostID, "text": text})
	message, ok := c.waitFor(90*time.Second, func(m map[string]any) bool {
		kind := str(m["type"])
		return str(m["request_id"]) == requestID && (kind == "input_sent" || kind == "input_failed" || kind == "input_pending")
	})
	if !ok {
		return "", fmt.Errorf("no answer to send_input")
	}
	if str(message["type"]) == "input_failed" {
		return "", fmt.Errorf("input_failed: %s: %s", str(message["code"]), str(message["message"]))
	}
	return str(message["type"]), nil
}

// waitReply waits for Brain's visible chat to hold an assistant message
// containing token.
func (e *env) waitReply(token string, timeout time.Duration) (string, bool) {
	db := filepath.Join(e.state, "brain", "state", "brain.db")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("sqlite3", "file:"+db+"?mode=ro", "select body from timeline order by seq").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				var item map[string]any
				if json.Unmarshal([]byte(line), &item) != nil {
					continue
				}
				body := str(item["body"])
				role := str(item["role"]) + str(item["kind"])
				if strings.Contains(body, token) && strings.Contains(role, "assistant") {
					return body, true
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", false
}

// askBrain sends a sum and waits for "<prefix>-<sum>" in Brain's reply.
func (e *env) askBrain(t *testing.T, c *client, hostID string, a, b int) (string, error) {
	prefix := fmt.Sprintf("SWEEP%d", time.Now().UnixNano()%100000)
	if _, err := chat(c, hostID, fmt.Sprintf("Compatibility check: reply with only %s-<the sum of %d and %d>, nothing else.", prefix, a, b)); err != nil {
		return "", err
	}
	want := fmt.Sprintf("%s-%d", prefix, a+b)
	reply, ok := e.waitReply(want, 4*time.Minute)
	if !ok {
		return "", fmt.Errorf("no reply holding %s", want)
	}
	return reply, nil
}

// TestBrainOnEachExecutor: Brain answers web Chat on claude, switches to
// codex with `mewla brain use` and answers there, and still answers after the
// daemon restarts, in the same Session.
func TestBrainOnEachExecutor(t *testing.T) {
	realAgent(t, "claude")
	realAgent(t, "codex")
	e := newRealEnv(t, realOptions{})
	executors := filepath.Join(e.state, "executors.toml")
	writeFile(t, executors, realExecutors, 0o600)
	e.start()
	device := e.pair()
	c := e.connect(device)
	step := func(name string, run func() (string, error)) bool {
		detail, err := run()
		if err != nil {
			record(t, name, "fail", err.Error()+"\n"+e.logTail(8))
			t.Errorf("%s: %v\n%s", name, err, e.logTail(30))
			return false
		}
		record(t, name, "pass", oneLine(detail))
		return true
	}
	ask := func(executor string, a, b int) func() (string, error) {
		return func() (string, error) {
			hostID, last := brainHost(c, executor, 3*time.Minute)
			if hostID == "" {
				return "", fmt.Errorf("no running %s Brain host; problem=%v", executor, find(last, "host_problem"))
			}
			reply, err := e.askBrain(t, c, hostID, a, b)
			return executor + " Brain replied: " + reply, err
		}
	}
	if !step("brain-claude-replies", ask("claude", 17, 25)) {
		return
	}
	step("brain-use-codex", func() (string, error) {
		out, err := e.cli(3*time.Minute, "brain", "use", "@state", "codex")
		if err != nil {
			return "", fmt.Errorf("brain use codex: %v: %s", err, oneLine(out))
		}
		return ask("codex", 30, 12)()
	})
	step("brain-survives-restart", func() (string, error) {
		before, _ := brainHost(c, "codex", time.Minute)
		e.stopServe()
		e.start()
		c = e.connect(device)
		after, _ := brainHost(c, "codex", 3*time.Minute)
		if before == "" || after != before {
			return "", fmt.Errorf("Brain's Session before the restart %q, after %q", before, after)
		}
		detail, err := ask("codex", 8, 9)()
		return "same Session " + after + "; " + detail, err
	})
	step("brain-use-claude-again", func() (string, error) {
		out, err := e.cli(3*time.Minute, "brain", "use", "@state", "claude")
		if err != nil {
			return "", fmt.Errorf("brain use claude: %v: %s", err, oneLine(out))
		}
		return ask("claude", 40, 2)()
	})
}
