package work

import "testing"

func TestIDForCommand(t *testing.T) {
	cfg := NewExecutorConfig(map[string]Executor{
		"claude":         {Name: "claude", Command: "claude"},
		"claude-auto":    {Name: "claude-auto", Command: "claude --permission-mode auto"},
		"agent":          {Name: "agent", Command: "cursor-agent --force"},
		"my-codex-proxy": {Name: "my-codex-proxy", Command: "proxy-wrapper codex"},
	})
	for command, want := range map[string]string{
		"claude":                                  "claude",
		"claude --permission-mode auto":           "claude-auto",
		"/usr/local/bin/claude --model x":         "claude",
		"cursor-agent --force --sandbox disabled": "agent",
		"proxy-wrapper codex":                     "my-codex-proxy",
		"codex":                                   "",
		"":                                        "",
	} {
		if got := cfg.IDForCommand(command); got != want {
			t.Errorf("IDForCommand(%q) = %q, want %q", command, got, want)
		}
	}
}
