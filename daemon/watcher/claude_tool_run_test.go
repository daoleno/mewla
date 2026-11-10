package watcher

import (
	"testing"
	"time"
)

// Seen on Windows: Claude's bundled ripgrep runs as claude.exe with the
// command line `rg …`, a child of the agent, for a moment at a time.
func TestClaudeBundledRipgrepIsNotTheAgent(t *testing.T) {
	for _, args := range []string{"rg --version", "rg --no-config --files --follow --hidden C:\\Users\\u\\projects\\hello", `"C:\Users\u\.local\bin\rg.exe" --files`} {
		if got := workerCommandFromProcess(processInfo{comm: "claude", args: args}); got != "" {
			t.Errorf("claude %q detected as %q, want no provider", args, got)
		}
	}
	agent := `C:\Users\u\.local\bin\claude.exe --permission-mode bypassPermissions --session-id e4454864-8777-410e-afc5-53ca0f5a34eb`
	if got := workerCommandFromProcess(processInfo{comm: "claude", args: agent}); got != "claude --session-id e4454864-8777-410e-afc5-53ca0f5a34eb" {
		t.Errorf("the agent itself = %q", got)
	}

	// The agent's identity holds while its ripgrep child comes and goes.
	start := time.Date(2026, 11, 9, 18, 24, 59, 0, time.UTC)
	processes := map[int]processInfo{
		7520: {pid: 7520, ppid: 1, pgid: 7520, comm: "mewla", args: "mewla host", startedAt: start.Add(-time.Hour)},
		7840: {pid: 7840, ppid: 7520, pgid: 7840, tpgid: 7840, comm: "sh", args: "mewla sh", startedAt: start},
	}
	// Before claude has started, the pane is in its launch shell, which is not
	// ready for input (on Windows the launcher is mewla sh, named sh).
	launching, ok := resolveForegroundTargetProcess(7840, processes)
	if !ok || !isTransientLaunchShell(launching.command) {
		t.Fatalf("launching = %q, %v; want the launch shell", launching.command, ok)
	}
	processes[9960] = processInfo{pid: 9960, ppid: 7840, pgid: 7840, comm: "claude", args: agent, startedAt: start.Add(25 * time.Millisecond)}
	before, ok := resolveForegroundTargetProcess(7840, processes)
	if !ok || before.provider.pid != 9960 {
		t.Fatalf("before = %+v, %v", before.provider, ok)
	}
	processes[9272] = processInfo{pid: 9272, ppid: 9960, pgid: 7840, comm: "claude", args: "rg --no-config --files", startedAt: start.Add(456 * time.Millisecond)}
	during, ok := resolveForegroundTargetProcess(7840, processes)
	if !ok || during.provider.pid != 9960 || during.provider.startedAt != before.provider.startedAt {
		t.Fatalf("with ripgrep running = %+v, %v; want the agent, 9960", during.provider, ok)
	}
}
