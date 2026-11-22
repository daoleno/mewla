package watcher

import "testing"

// Only Mewla's own `mewla agy-session --agy-session agy-<uuid>` is an
// Antigravity Session program; an agent whose command line merely mentions agy
// stays that agent.
func TestProcessScanTakesOnlyMewlasAgySessionProgram(t *testing.T) {
	const session = "/opt/mewla agy-session --agy-session agy-0b6a3c1e-0000-4000-8000-00000000abcd --state-dir /s --model gemini-4-neon-low"
	if got := workerCommandFromProcess(processInfo{comm: "mewla", args: session}); got != session {
		t.Fatalf("the Session program = %q, want its command line", got)
	}
	for _, c := range []struct{ comm, args, want string }{
		{"claude", "claude --append-system-prompt use agy agy-session --agy-session", "claude"},
		{"codex", "codex exec run agy acp then mewla agy-session", "codex"},
		{"pi", "pi --model agy agy-session", "pi"},
	} {
		if got := workerCommandFromProcess(processInfo{comm: c.comm, args: c.args}); got == c.args || got == "" || workerCommandName(got) != c.want {
			t.Errorf("%s argv %q = %q, want a %s command", c.comm, c.args, got, c.want)
		}
	}
}
