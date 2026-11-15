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

// Seen on Windows: a Claude Brain starts a Codex Work with `mewla worker
// spawn -executor codex`, run through cmd and PowerShell. Every process under
// the launcher is in the foreground there, and the command line names codex
// as an option value; the Brain's agent is still Claude. A mewla process is an
// agent only as dsh-session.
func TestMewlaCommandsAnAgentRunsAreNotAgents(t *testing.T) {
	for _, args := range []string{
		`C:\Users\u\AppData\Local\Programs\Mewla\mewla.exe worker spawn -name 'Create work-test2' -executor codex -cwd 'C:\Users\u' -prompt 'write hello.txt'`,
		`/usr/local/bin/mewla worker spawn -executor codex -prompt x`,
		`C:\Users\u\AppData\Local\Programs\Mewla\mewla.exe codex-server-wait C:\Users\u\.mewla\codex-ctl\codex-ctl-1.sock`,
	} {
		if got := workerCommandFromProcess(processInfo{comm: "mewla", args: args}); got != "" {
			t.Errorf("mewla %q detected as %q, want no provider", args, got)
		}
	}
	dsh := "mewla dsh-session --model deepseek"
	if got := workerCommandFromProcess(processInfo{comm: "mewla", args: dsh}); got != dsh {
		t.Errorf("dsh-session = %q, want %q", got, dsh)
	}

	start := time.Date(2026, 11, 15, 2, 7, 2, 0, time.UTC)
	agent := `C:\Users\u\.local\bin\claude.exe --permission-mode bypassPermissions --session-id 86a23010-0000-4000-8000-000000000001`
	processes := map[int]processInfo{
		12148: {pid: 12148, ppid: 1, pgid: 12148, comm: "mewla", args: "mewla host", startedAt: start.Add(-time.Hour)},
		9252:  {pid: 9252, ppid: 12148, pgid: 9252, tpgid: 9252, comm: "sh", args: "mewla sh", startedAt: start},
		9888:  {pid: 9888, ppid: 9252, pgid: 9252, comm: "claude", args: agent, startedAt: start.Add(time.Second)},
		9348:  {pid: 9348, ppid: 9888, pgid: 9252, comm: "cmd", args: `C:\Windows\system32\cmd.exe /d /s /c "pwsh -NoProfile -Command ..."`, startedAt: start.Add(6 * time.Minute)},
		10012: {pid: 10012, ppid: 9348, pgid: 9252, comm: "conhost", args: `\??\C:\Windows\system32\conhost.exe 0xffffffff -ForceV1`, startedAt: start.Add(6 * time.Minute)},
		2436:  {pid: 2436, ppid: 9348, pgid: 9252, comm: "pwsh", args: "pwsh -NoProfile -Command ...", startedAt: start.Add(6 * time.Minute)},
		1780:  {pid: 1780, ppid: 2436, pgid: 9252, comm: "mewla", args: `C:\Users\u\AppData\Local\Programs\Mewla\mewla.exe worker spawn -name 'Create work-test2' -executor codex -cwd 'C:\Users\u' -prompt 'write hello.txt'`, startedAt: start.Add(6*time.Minute + time.Second)},
	}
	got, ok := resolveForegroundTargetProcess(9252, processes)
	if !ok || got.provider.pid != 9888 {
		t.Fatalf("Brain while it spawns a Work = %+v, %v; want Claude, 9888", got.provider, ok)
	}
}
