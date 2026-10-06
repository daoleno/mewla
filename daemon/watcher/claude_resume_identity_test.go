package watcher

import "testing"

func TestClaudeProcessIdentityPreservesTranscriptSession(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{"claude --model model --resume resumed-session --settings '{}'", "claude --resume resumed-session"},
		{"claude -r short-session", "claude --resume short-session"},
		{"claude --session-id new-session", "claude --session-id new-session"},
		{"claude --resume=equals-session", "claude --resume equals-session"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			p := processInfo{pid: 1, comm: "claude", args: tc.args}
			got := workerCommandFromProcess(p)
			if got != tc.want {
				t.Fatalf("provider command lost exact transcript identity: got %q want %q", got, tc.want)
			}
			if workerProcessScore(p, got) != 100 {
				t.Fatal("Claude native process lost selection priority")
			}
		})
	}
}
