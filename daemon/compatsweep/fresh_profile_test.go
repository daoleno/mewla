//go:build linux || darwin

package compatsweep

import (
	"fmt"
	"testing"
	"time"
)

// TestFreshProfiles: an agent on a computer where it never ran (no theme,
// onboarding or API-key answer recorded) and one nobody signed in to. Brain
// either answers or says, in the client's event stream, what it waits for;
// silence fails.
func TestFreshProfiles(t *testing.T) {
	cases := []struct {
		name     string
		executor string
		opts     realOptions
	}{
		{"fresh-profile-claude", "claude", realOptions{fresh: true}},
		{"fresh-profile-codex", "codex", realOptions{fresh: true}},
		{"signed-out-claude", "claude", realOptions{fresh: true, signedOut: true}},
		{"signed-out-codex", "codex", realOptions{fresh: true, signedOut: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			realAgent(t, tc.executor)
			opts := tc.opts
			opts.executor = tc.executor
			e := newRealEnv(t, opts)
			e.start()
			c := e.connect(e.pair())
			outcome, detail := e.freshOutcome(t, c, tc.executor)
			record(t, tc.name, outcome, detail)
			if outcome == "fail" {
				t.Error(detail)
			}
		})
	}
}

// freshOutcome is "pass" when Brain answered, or when the client was told
// why it cannot (a host problem with a detail), and "fail" on silence.
func (e *env) freshOutcome(t *testing.T, c *client, executor string) (string, string) {
	deadline := time.Now().Add(4 * time.Minute)
	asked := ""
	token := ""
	for time.Now().Before(deadline) {
		hostID, last := brainHost(c, executor, 10*time.Second)
		if problem, ok := find(last, "host_problem").(map[string]any); ok && str(problem["detail"]) != "" {
			return "pass", fmt.Sprintf("said %s: %s", str(problem["code"]), str(problem["detail"]))
		}
		if hostID != "" && asked == "" {
			token = fmt.Sprintf("SWEEPFRESH%d", time.Now().UnixNano()%100000)
			if _, err := chat(c, hostID, "Compatibility check: reply with only "+token+", nothing else."); err == nil {
				asked = hostID
			}
		}
		if token != "" {
			if reply, ok := e.waitReply(token, 5*time.Second); ok {
				return "pass", executor + " Brain answered on a first run: " + oneLine(reply)
			}
		}
	}
	return "fail", fmt.Sprintf("no answer and no problem in 4 min (asked=%q)\n%s", asked, e.logTail(12))
}
