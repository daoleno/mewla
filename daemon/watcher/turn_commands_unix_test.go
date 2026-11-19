//go:build !windows

package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

func matchTurnCmd(args string) (string, bool) {
	_, command, ok := strings.Cut(args, "turn-cmd:")
	return strings.TrimSpace(command), ok
}

func TestTurnCommandGroupsAreTheAgentsOwnGroupsStartedThisTurn(t *testing.T) {
	since := time.Date(2026, 11, 19, 12, 0, 10, 0, time.UTC)
	starts := map[int]time.Time{
		101: since.Add(-5 * time.Second),        // an earlier turn's command
		102: since.Add(2 * time.Second),         // this turn's command
		103: since.Add(3 * time.Second),         // its child, in its group
		104: since.Add(time.Second),             // a server in the agent's own group
		105: since.Add(time.Second),             // a hook: not a command
		106: since.Add(4 * time.Second),         // the command's own setsid child
		107: since.Add(-100 * time.Millisecond), // within the start's rounding
	}
	processes := map[int]processInfo{
		100: {pid: 100, ppid: 1, pgid: 100, args: "claude"},
		101: {pid: 101, ppid: 100, pgid: 101, args: "zsh -c turn-cmd: sleep 300"},
		102: {pid: 102, ppid: 100, pgid: 102, args: "zsh -c turn-cmd: seq 1 30"},
		103: {pid: 103, ppid: 102, pgid: 102, args: "sleep 1"},
		104: {pid: 104, ppid: 100, pgid: 100, args: "zsh -c turn-cmd: mcp"},
		105: {pid: 105, ppid: 100, pgid: 105, args: "telegram-notify.sh question"},
		106: {pid: 106, ppid: 102, pgid: 106, args: "zsh -c turn-cmd: nested"},
		107: {pid: 107, ppid: 100, pgid: 107, args: "zsh -c turn-cmd: just now"},
	}
	startOf := func(pid int) (time.Time, bool) { start, ok := starts[pid]; return start, ok }
	groups := turnCommandGroups(100, processes, since, matchTurnCmd, startOf)
	var got []string
	for _, group := range groups {
		var members []int
		for _, member := range group.members {
			members = append(members, member.pid)
		}
		sort.Ints(members)
		got = append(got, fmt.Sprint(group.command, members))
	}
	want := []string{"just now[107]", "seq 1 30[102 103]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
}

func TestSettleTurnCommandsKillsWhatOutlivesTheGraceAndSaysWhatStayed(t *testing.T) {
	alive := map[int]bool{1: false, 2: true, 3: true}
	killed := map[int]bool{}
	groups := []turnCommandGroup{
		{command: "quits on TERM", leader: turnCommandProcess{pid: 1}},
		{command: "ignores TERM", leader: turnCommandProcess{pid: 2}},
		{command: "unkillable", leader: turnCommandProcess{pid: 3}},
		{command: "not permitted", leader: turnCommandProcess{pid: 4}, refused: "operation not permitted"},
	}
	stopped := settleTurnCommands(groups,
		func(group turnCommandGroup) bool { return alive[group.leader.pid] },
		func(group turnCommandGroup, final bool) error {
			if final {
				killed[group.leader.pid] = true
				if group.leader.pid == 2 {
					alive[2] = false
				}
			}
			return nil
		},
		func(time.Duration) {})
	want := []agentevents.StoppedCommand{
		{Command: "quits on TERM", Ended: true},
		{Command: "ignores TERM", Ended: true},
		{Command: "unkillable", Reason: "it was still running after SIGKILL"},
		{Command: "not permitted", Reason: "the system refused to end it: operation not permitted"},
	}
	if !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stopped = %+v, want %+v", stopped, want)
	}
	if killed[1] || !killed[2] || !killed[3] || killed[4] {
		t.Fatalf("SIGKILL went to %v, want only the groups still running", killed)
	}
}

// TestStopTurnCommandsEndsOnlyTheGroupStartedThisTurn runs a fake agent with
// two setsid children, one started before the turn and one after: the Stop
// ends the second by its process group, child included, and leaves the first.
func TestStopTurnCommandsEndsOnlyTheGroupStartedThisTurn(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("no setsid on this system")
	}
	dir := t.TempDir()
	goFile := filepath.Join(dir, "go")
	// The agent starts the earlier turn's command, waits for the go file,
	// then starts this turn's, whose loop has a child of its own.
	script := `setsid sh -c 'echo turn-cmd: earlier; while :; do sleep 1; done' &
while [ ! -e ` + goFile + ` ]; do sleep 0.05; done
setsid sh -c 'echo turn-cmd: count; while :; do sleep 1; done' &
wait`
	agent := exec.Command("sh", "-c", script)
	agent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-agent.Process.Pid, syscall.SIGKILL)
		_ = agent.Wait()
	}()
	earlier := waitForChild(t, agent.Process.Pid, "earlier")
	defer syscall.Kill(-earlier, syscall.SIGKILL)
	time.Sleep(600 * time.Millisecond)
	since := time.Now()
	if err := os.WriteFile(goFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	count := waitForChild(t, agent.Process.Pid, "count")
	defer syscall.Kill(-count, syscall.SIGKILL)
	// The count's own sleep is in its group.
	time.Sleep(200 * time.Millisecond)

	reports := make(chan []agentevents.StoppedCommand, 1)
	match := func(args string) (string, bool) {
		_, rest, ok := strings.Cut(args, "turn-cmd: ")
		if !ok {
			return "", false
		}
		word, _, _ := strings.Cut(rest, ";")
		return word, true
	}
	w := &Watcher{}
	started := time.Now()
	w.stopTurnCommands(agent.Process.Pid, TurnCommandScope{Since: since, Match: match, Report: func(stopped []agentevents.StoppedCommand) { reports <- stopped }})
	select {
	case stopped := <-reports:
		want := []agentevents.StoppedCommand{{Command: "count", Ended: true}}
		if !reflect.DeepEqual(stopped, want) {
			t.Fatalf("stopped = %+v, want %+v", stopped, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report within 5 s")
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("a command that ends on SIGTERM took %s to report", took)
	}
	if err := syscall.Kill(-count, 0); err == nil {
		t.Fatal("this turn's command group is still running")
	}
	if err := syscall.Kill(-earlier, 0); err != nil {
		t.Fatalf("the earlier turn's command was ended: %v", err)
	}
}

// waitForChild returns the pid of agent's direct child whose command line
// names tag.
func waitForChild(t *testing.T, agent int, tag string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := snapshotProcessTable()
		if err == nil {
			for _, process := range processes {
				if process.ppid == agent && process.pgid == process.pid && strings.Contains(process.args, "turn-cmd: "+tag) {
					return process.pid
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no child %q of %d", tag, agent)
	return 0
}
