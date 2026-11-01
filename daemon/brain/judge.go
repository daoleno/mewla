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
// without tools, hooks or a saved session, in an empty scratch directory, so
// it neither acts nor leaves a transcript Mewla would scan. It reaches the
// model the way the Brain Host does: the Host's own --settings (its model
// route and credentials) are passed through, because the daemon's environment
// carries no login of its own. It supports Claude Code; with another Host
// agent it reports unavailable and Work stays as it is.
type headlessJudge struct {
	hostCommand func() string
	scratch     string
}

// NewHeadlessJudge returns the production WorkJudge. hostCommand is the
// command line the Brain Host runs; scratch is an empty directory the CLI
// runs in.
func NewHeadlessJudge(hostCommand func() string, scratch string) WorkJudge {
	return &headlessJudge{hostCommand: hostCommand, scratch: scratch}
}

func (j *headlessJudge) Judge(ctx context.Context, brief string) (JudgeVerdict, error) {
	command := j.hostCommand()
	fields := strings.Fields(command)
	if len(fields) == 0 || work.InferWorkerProvider(command) != "claude" {
		return JudgeVerdict{}, fmt.Errorf("headless judge supports Claude Code; Brain runs %q", command)
	}
	if err := os.MkdirAll(j.scratch, 0o700); err != nil {
		return JudgeVerdict{}, err
	}
	args := []string{"-p", "--model", judgeModel, "--no-session-persistence",
		"--tools", "", "--disable-slash-commands", "--setting-sources", "", "--strict-mcp-config", "--output-format", "text"}
	if settings := commandFlagJSON(fields, "--settings"); settings != "" {
		args = append(args, "--settings", settings)
	}
	cmd := exec.CommandContext(ctx, fields[0], args...)
	cmd.Dir = j.scratch
	cmd.Stdin = strings.NewReader(brief)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return JudgeVerdict{}, fmt.Errorf("headless judge: %w: %s%s", err, strings.TrimSpace(judgeClip(stderr.String(), 200)), strings.TrimSpace(judgeClip(stdout.String(), 200)))
	}
	return parseJudgeVerdict(stdout.String())
}

// commandFlagJSON returns the JSON object that follows flag in a command line
// split on whitespace, rejoining the pieces until the braces balance.
func commandFlagJSON(fields []string, flag string) string {
	for i, field := range fields {
		if field != flag || i+1 >= len(fields) || !strings.HasPrefix(fields[i+1], "{") {
			continue
		}
		depth := 0
		var b strings.Builder
		for _, piece := range fields[i+1:] {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(piece)
			depth += strings.Count(piece, "{") - strings.Count(piece, "}")
			if depth <= 0 {
				return b.String()
			}
		}
	}
	return ""
}
