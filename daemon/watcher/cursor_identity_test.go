package watcher

import (
	"testing"
	"time"
)

func TestWorkerProcessIdentityIgnoresClaudeModelArguments(t *testing.T) {
	for _, test := range []struct {
		name string
		comm string
		args string
		want string
	}{
		{"cursor opus", "MainThread", "/home/daoleno/.local/bin/cursor-agent --use-system-ca /home/daoleno/.local/share/cursor-agent/versions/2026.10.01-e373342/index.js --model claude-opus-5-5-high --force", "cursor-agent"},
		{"cursor sonnet", "node", "node /opt/bin/cursor-agent --model claude-sonnet-4-6", "cursor-agent"},
		{"codex model", "node", "node /opt/bin/codex --model claude-opus-5-5-high", "codex"},
		{"generic model", "MainThread", "node /repo/server.js --model claude-opus-5-5-high", ""},
		{"native claude", "claude", "claude --model opus", "claude"},
		{"claude wrapper", "node", "node /opt/bin/claude --model opus", "claude"},
	} {
		t.Run(test.name, func(t *testing.T) {
			proc := processInfo{pid: 20, ppid: 10, comm: test.comm, args: test.args, startedAt: time.Now()}
			if got := workerCommandFromProcess(proc); got != test.want {
				t.Fatalf("process command = %q, want %q", got, test.want)
			}
			if test.want == "" {
				return
			}
			command, _, pid := detectWorkerProcess("zsh", 10, map[int]processInfo{
				10: {pid: 10, comm: "zsh", args: "zsh"},
				20: proc,
			}, time.Now())
			if command != test.want || pid != 20 {
				t.Fatalf("detected command=%q pid=%d, want command=%q pid=20", command, pid, test.want)
			}
		})
	}
}
