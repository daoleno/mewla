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

// judgeModel is the one cheap model the headless judge asks.
const judgeModel = "claude-haiku-5-5"

// headlessJudge asks a cheap model through the user's own agent CLI, run once
// without tools, hooks, settings or a saved session, in an empty scratch
// directory, so it neither acts nor leaves a transcript Mewla would scan. It
// supports Claude Code; with another Host agent it reports unavailable and
// Work stays as it is.
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
	if work.InferWorkerProvider(executor.Command) != "claude" {
		return JudgeVerdict{}, fmt.Errorf("headless judge supports Claude Code; Brain runs %q", executor.ID)
	}
	binary := strings.Fields(executor.Command)[0]
	if err := os.MkdirAll(j.scratch, 0o700); err != nil {
		return JudgeVerdict{}, err
	}
	cmd := exec.CommandContext(ctx, binary, "-p", "--model", judgeModel, "--no-session-persistence",
		"--tools", "", "--disable-slash-commands", "--setting-sources", "", "--strict-mcp-config", "--output-format", "text")
	cmd.Dir = j.scratch
	cmd.Stdin = strings.NewReader(brief)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return JudgeVerdict{}, fmt.Errorf("headless judge: %w: %s", err, strings.TrimSpace(judgeClip(stderr.String(), 300)))
	}
	return parseJudgeVerdict(stdout.String())
}
