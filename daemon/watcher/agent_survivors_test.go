package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// What a dead agent left running is told without what may be a secret.
func TestSurvivorCommandTellsNoSecret(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"/usr/bin/python3", "-m", "http.server", "18473"}, "python3 -m http.server 18473"},
		// Mewla's own Claude launch carries its route's key in --settings.
		{[]string{"/home/u/.local/bin/claude", "--model", "claude-sonnet-5-5", "--settings", `{"env":{"ANTHROPIC_API_KEY":"sk-ant-mewla-0123456789abcdef"}}`},
			"claude --model claude-sonnet-5-5 --settings …"},
		{[]string{"curl", "-H", "Authorization: Bearer abc", "--token=ghp_0123456789abcdefABCDEF", "https://example.com/x"},
			"curl -H … --token=… https://example.com/x"},
		{[]string{"env", "OPENAI_API_KEY=sk-proj-0123456789", "node", "deploy.js"}, "env OPENAI_API_KEY=… node deploy.js"},
		{[]string{"deploy", "sk-ant-api03-abcdef"}, "deploy …"},
		{[]string{"tool", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"}, "tool …"},
		{[]string{"tool", "--api-key", "plain"}, "tool --api-key …"},
	} {
		if got := survivorCommand(test.args); got != test.want {
			t.Errorf("survivorCommand(%q) = %q, want %q", test.args, got, test.want)
		}
	}
	long := survivorCommand([]string{"x", strings.Repeat("a/", 200)})
	if len([]rune(long)) > survivorLineMax {
		t.Fatalf("a %d-rune line", len([]rune(long)))
	}
}

func TestSurvivorListSaysHowLongEachRan(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	var survivors []agentSurvivor
	for index := range survivorListMax + 2 {
		survivors = append(survivors, agentSurvivor{PID: 100 + index, Command: "sleep 600", Started: now.Add(-12 * time.Minute)})
	}
	list := survivorList(survivors, now)
	lines := strings.Split(list, "\n")
	if len(lines) != survivorListMax+1 || lines[0] != "- sleep 600 (running 12m)" || lines[survivorListMax] != "- and 2 more" {
		t.Fatalf("list = %q", list)
	}
	if words := survivorWords(survivors[:2], false, now); words != " Still running from before: sleep 600 (running 12m); sleep 600 (running 12m)." {
		t.Fatalf("words = %q", words)
	}
}

// Survivors lists a process the unit owns and leaves it running: a relaunch
// stops nothing it did not start.
func TestPortableSurvivorsListsAndLeavesTheUnitsProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process ownership is read from /proc")
	}
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := workerproc.LeasePath(leaseDir, unit)
	if err != nil {
		t.Fatal(err)
	}
	if err := workerproc.WriteOwnershipLease(path, unit); err != nil {
		t.Fatal(err)
	}
	manager := &portableDelegatedResourceManager{owner: owner, leaseDir: leaseDir, tempRoot: root,
		byTarget: map[string]string{}, reserved: map[string]time.Time{}, now: time.Now}
	server := exec.Command("sleep", "300")
	server.Env = append(os.Environ(), delegatedResourceUnitEnv+"="+unit)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	var survivors []agentSurvivor
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if survivors, err = manager.Survivors(unit); err == nil && len(survivors) == 1 {
			break
		}
	}
	if len(survivors) != 1 {
		t.Fatalf("survivors = %+v, %v", survivors, err)
	}
	if survivors[0].PID != server.Process.Pid || survivors[0].Command != "sleep 300" || survivors[0].Started.IsZero() {
		t.Fatalf("survivors = %+v", survivors)
	}
	if err := server.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("listing stopped the process: %v", err)
	}
}

// The agent's control server and what runs under it are the agent's own, not
// something it left running; the rest is listed.
func TestSurvivorsLeaveOutTheControlServerAndItsChildren(t *testing.T) {
	socket := "/state/codex-ctl/codex-ctl-1.sock"
	survivors := []agentSurvivor{
		{PID: 10, ppid: 1, args: []string{"codex", "app-server", "--listen", "unix://" + socket}},
		{PID: 11, ppid: 10, args: []string{"python", "mcp-server"}},
		{PID: 12, ppid: 11, args: []string{"node", "tool"}},
		{PID: 20, ppid: 1, args: []string{"python3", "-m", "http.server"}},
		{PID: 21, ppid: 99, args: []string{"sleep", "60"}},
	}
	got := withoutControlServer(survivors, socket)
	if len(got) != 2 || got[0].PID != 20 || got[1].PID != 21 {
		t.Fatalf("survivors = %+v", got)
	}
}

// endingManager is a resource manager whose units end with their Session, as
// Windows' Job Objects do.
type endingManager struct{ noopDelegatedResourceManager }

func (endingManager) EndsSurvivors() bool { return true }

// On Windows a relaunch closes the old Session's Job Object, which ends what
// the Worker had left running: nothing is listed, and Brain's event and the
// continuation say it was stopped, never "Mewla stopped nothing".
func TestARelaunchThatEndsWhatTheAgentLeftSaysSo(t *testing.T) {
	survivors, stopped := unitSurvivors(endingManager{}, "unit-1", "")
	if len(survivors) != 0 || !stopped {
		t.Fatalf("survivors %+v stopped %v", survivors, stopped)
	}
	if words := survivorWords(survivors, stopped, time.Now()); words != " What it had left running was stopped: on Windows it ends with the old Session." {
		t.Fatalf("Brain's words = %q", words)
	}
	prompt := AgentContinuationPrompt("was killed (SIGKILL)", "", stopped)
	if !strings.Contains(prompt, "everything it had left running was stopped with it") || strings.Contains(prompt, "Mewla stopped nothing") ||
		strings.Contains(prompt, "may still be running") {
		t.Fatalf("continuation = %q", prompt)
	}
	// A manager that keeps its units (Linux, macOS) stops nothing.
	if _, stopped := unitSurvivors(noopDelegatedResourceManager{}, "unit-1", ""); stopped {
		t.Fatal("a unit that outlives its Session reported as stopped")
	}
}
