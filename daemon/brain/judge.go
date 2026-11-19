package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/work"
)

// judgeModel is the one cheap model the headless judge asks. It matches the
// Watch judgment (watch/judge_agents.go): Haiku is not available to every
// subscription.
const judgeModel = "sonnet"

// headlessJudge asks a cheap model through the user's own agent CLI, run once
// the way Watch's judgment runs it: print mode in an empty directory with no
// tools, hooks, MCP servers or saved session, and only the user's own
// settings, because those carry how this person signs in. It supports Claude
// Code; with another Host agent it reports unavailable and Work stays as it is.
type headlessJudge struct {
	executor func() work.WorkerExecutor
	scratch  string
}

// NewHeadlessJudge returns the production WorkJudge. executor names the agent
// the Brain Host runs on; scratch is an empty directory the CLI runs in.
func NewHeadlessJudge(executor func() work.WorkerExecutor, scratch string) WorkJudge {
	return &headlessJudge{executor: executor, scratch: scratch}
}

func (j *headlessJudge) Judge(ctx context.Context, brief string) (JudgeVerdict, error) {
	executor := j.executor()
	fields := strings.Fields(executor.Command)
	if len(fields) == 0 || work.InferWorkerProvider(executor.Command) != "claude" {
		return JudgeVerdict{}, fmt.Errorf("headless judge supports Claude Code; Brain runs %q", executor.Command)
	}
	if err := os.MkdirAll(j.scratch, 0o700); err != nil {
		return JudgeVerdict{}, err
	}
	cmd := exec.CommandContext(ctx, fields[0], "-p", "--model", judgeModel, "--tools", "", "--no-session-persistence",
		"--setting-sources", "user", "--settings", `{"disableAllHooks":true}`, "--strict-mcp-config", "--output-format", "text")
	cmd.Dir = j.scratch
	cmd.Stdin = strings.NewReader(brief)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return JudgeVerdict{}, judgeRunError(ctx, err, stdout.String()+" "+stderr.String(), judgeRoute(modelprofiles.DefaultClaudeSettingsPath()))
	}
	return parseJudgeVerdict(stdout.String())
}

// judgeAPIErrorRe is how Claude Code prints an upstream refusal in print mode:
// "API Error: 403 Insufficient account balance". It prefixes any 403 with
// "Failed to authenticate.", whatever the upstream meant.
var judgeAPIErrorRe = regexp.MustCompile(`API Error: (\d{3})\b[\s·:-]*([^\n]*)`)

// judgeRunError says why the judge could not answer: the route it went
// through and what that route answered, or that it timed out or was killed.
func judgeRunError(ctx context.Context, err error, output, route string) error {
	output = strings.TrimSpace(output)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("headless judge timed out after %s (route: %s)", judgeTimeout, route)
	}
	if match := judgeAPIErrorRe.FindStringSubmatch(output); match != nil {
		return fmt.Errorf("headless judge: %s answered HTTP %s: %s", route, match[1], judgeClip(match[2], 200))
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && !exit.Exited() {
		return fmt.Errorf("headless judge was stopped by %v (out of memory or stopped by the system; route: %s)", err, route)
	}
	return fmt.Errorf("headless judge: %w (route: %s): %s", err, route, judgeClip(output, 300))
}

// judgeRoute names where the judge's headless Claude sends its request: the
// base URL in the user's settings (Mewla's gateway when the Claude takeover is
// on), else the one in the daemon's environment, else Claude Code's sign-in.
func judgeRoute(settingsPath string) string {
	raw, err := os.ReadFile(settingsPath)
	if err == nil {
		var settings struct {
			Env map[string]any `json:"env"`
		}
		if json.Unmarshal(raw, &settings) == nil {
			if base, ok := settings.Env["ANTHROPIC_BASE_URL"].(string); ok && strings.TrimSpace(base) != "" {
				return "the model route " + strings.TrimSpace(base) + " (from " + settingsPath + ")"
			}
		}
	}
	if base := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); base != "" {
		return "the model route " + base + " (from the daemon's environment)"
	}
	return "Claude Code's own sign-in"
}
