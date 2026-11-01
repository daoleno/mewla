package brain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		return JudgeVerdict{}, fmt.Errorf("headless judge: %w: %s%s", err, strings.TrimSpace(judgeClip(stderr.String(), 200)), strings.TrimSpace(judgeClip(stdout.String(), 200)))
	}
	return parseJudgeVerdict(stdout.String())
}
