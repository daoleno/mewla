//go:build linux || darwin

package compatsweep

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// hostProblem waits for a brain snapshot whose host problem match accepts.
func hostProblem(c *client, timeout time.Duration, match func(problem map[string]any) bool) (map[string]any, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.send(map[string]any{"type": "brain_snapshot"})
		message, ok := c.waitFor(3*time.Second, func(m map[string]any) bool {
			for _, key := range [][]string{{"snapshot", "host_problem"}, {"brain", "host_problem"}, {"host_problem"}} {
				if problem, ok := path(m, key...).(map[string]any); ok && match(problem) {
					return true
				}
			}
			return false
		})
		if ok {
			for _, key := range [][]string{{"snapshot", "host_problem"}, {"brain", "host_problem"}, {"host_problem"}} {
				if problem, ok := path(message, key...).(map[string]any); ok {
					return problem, true
				}
			}
		}
	}
	return nil, false
}

// TestBrainAgentThatExitsAtLaunch is the Windows 0.4.6 npm claude.cmd case on
// any OS: Brain's agent exits with code 3 at once, printing why. The cause
// reaches the client's event stream and the log, and Mewla stops launching it.
func TestBrainAgentThatExitsAtLaunch(t *testing.T) {
	e := newEnv(t, envOptions{
		executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
		hostExecutor: "claude",
	})
	writeFile(t, e.tools+"/claude", "#!/bin/sh\n[ \"$1\" = --version ] && { echo '2.1.325 (Claude Code)'; exit 0; }\n"+
		"echo 'Error: boom from fake claude: cannot start' >&2\nexit 3\n", 0o755)
	e.start()
	c := e.connect(e.pair())
	problem, ok := hostProblem(c, 90*time.Second, func(p map[string]any) bool {
		return str(p["code"]) == "start_failed" && strings.Contains(str(p["detail"]), "boom from fake claude")
	})
	if !ok {
		record(t, "brain-agent-exits-at-launch", "fail", "no start_failed host problem with the agent's output reached the client")
		t.Fatalf("no start_failed host problem with the output; types %v\n%s", c.types(), e.logTail(30))
	}
	if !strings.Contains(str(problem["detail"]), "exit code 3") {
		t.Errorf("the problem does not say the exit code: %v", problem["detail"])
	}
	line, logged := e.waitLog(`brain host exited at start \(claude\): .* exit=exit code 3 .*boom from fake claude`, 10*time.Second)
	if !logged {
		t.Errorf("no launch-failed log line:\n%s", e.logTail(30))
	}
	// It is not started again every few seconds: one launch in the next
	// minute (the retry waits two minutes, and stops after the same end twice).
	time.Sleep(60 * time.Second)
	launches := strings.Count(e.logText(), "brain host exited at start (claude)")
	if launches > 1 {
		t.Errorf("launched %d times in a minute, want 1:\n%s", launches, e.logTail(40))
	}
	// brain use names the cause.
	out, _ := e.cli(90*time.Second, "brain", "use", "@state", "claude")
	if !strings.Contains(out, "exit code 3") || !strings.Contains(out, "boom from fake claude") {
		t.Errorf("brain use output does not name the cause:\n%s", out)
	}
	record(t, "brain-agent-exits-at-launch", map[bool]string{true: "fail", false: "pass"}[t.Failed()],
		fmt.Sprintf("problem=%q; log=%q; launches=%d; brain use=%q", str(problem["detail"]), line, launches, strings.TrimSpace(out)))
	e.hold()
}
