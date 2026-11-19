package watcher

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// A keystroke Stop (Ctrl-C into the pane) reaches only the pane's foreground
// process group. An agent that runs each command as a direct child in a group
// of its own (Claude's Bash tool) keeps those running, background ones until
// the agent itself ends them. TurnCommandScope lets the Stop end the ones the
// interrupted turn started, by their process group, and nothing else.

const (
	// turnCommandGrace is how long a command's group has between SIGTERM and
	// SIGKILL.
	turnCommandGrace = 2 * time.Second
	// turnCommandSettle is how long a group has to be gone after SIGKILL.
	turnCommandSettle = time.Second
	// turnCommandStartSlack absorbs the rounding of a process start time (a
	// clock tick on Linux) against the turn's start.
	turnCommandStartSlack = 250 * time.Millisecond
	turnCommandPoll       = 100 * time.Millisecond
)

// TurnCommandScope is the running turn a keystroke Stop interrupts and how to
// find what it started.
type TurnCommandScope struct {
	// Since is when the turn started: a command that started before it is an
	// earlier turn's and is left alone.
	Since time.Time
	// Match reads a direct child's command line: ok only for a command the
	// agent ran for its conversation (agentevents.TurnCommandMatcher).
	Match func(args string) (command string, ok bool)
	// Report receives what the Stop did to each command it found; it is not
	// called when the turn started none.
	Report func(stopped []agentevents.StoppedCommand)
	// Agent names the agent and its version for the warning logged when
	// Match recognizes none of the turn's processes (see
	// unrecognizedTurnChildren).
	Agent func() string
}

// SetTurnCommandStop installs how a keystroke Stop finds the turn it
// interrupts. scope reports ok=false for a Session whose agent does not
// declare its commands, or that has no running turn; Ctrl-C is then all a
// Stop does.
func (w *Watcher) SetTurnCommandStop(scope func(sessionID string) (TurnCommandScope, bool)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.turnCommandScope = scope
}

func (w *Watcher) turnCommandScopeFor(sessionID string) (TurnCommandScope, bool) {
	w.mu.RLock()
	scope := w.turnCommandScope
	w.mu.RUnlock()
	if scope == nil {
		return TurnCommandScope{}, false
	}
	found, ok := scope(sessionID)
	if !ok || found.Since.IsZero() || found.Match == nil {
		return TurnCommandScope{}, false
	}
	return found, true
}

// turnCommandProcess is one process of a command, with the start that tells
// it from a later process given the same pid.
type turnCommandProcess struct {
	pid   int
	start time.Time
}

// turnCommandGroup is one command the turn started: its leader (the agent's
// direct child) and every process in its group.
type turnCommandGroup struct {
	command string
	leader  turnCommandProcess
	members []turnCommandProcess
	// refused is why the group could not be signalled, if it could not.
	refused string
}

// turnCommandGroups finds the commands the turn started: direct children of
// the agent that lead a process group of their own, that the agent's adapter
// recognizes as its commands, and that started at or after since. startOf
// reads a process's precise start.
func turnCommandGroups(agentPID int, processes map[int]processInfo, since time.Time, match func(string) (string, bool), startOf func(int) (time.Time, bool)) []turnCommandGroup {
	floor := since.Add(-turnCommandStartSlack)
	var groups []turnCommandGroup
	for _, child := range processes {
		if child.ppid != agentPID || child.pid == agentPID || child.pid <= 0 {
			continue
		}
		if processGroupsAreReal && child.pgid != child.pid {
			continue
		}
		command, ok := match(child.args)
		if !ok {
			continue
		}
		start, ok := startOf(child.pid)
		if !ok || start.Before(floor) {
			continue
		}
		group := turnCommandGroup{command: command, leader: turnCommandProcess{pid: child.pid, start: start}}
		for _, member := range commandGroupMembers(child, processes) {
			if memberStart, ok := startOf(member.pid); ok {
				group.members = append(group.members, turnCommandProcess{pid: member.pid, start: memberStart})
			}
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if !groups[i].leader.start.Equal(groups[j].leader.start) {
			return groups[i].leader.start.Before(groups[j].leader.start)
		}
		return groups[i].leader.pid < groups[j].leader.pid
	})
	return groups
}

// unrecognizedTurnChildren are the agent's direct children that look like
// this turn's commands (a process group of their own, started this turn) but
// that match does not read as one, leaving out Mewla's own (its hooks, its MCP
// servers), named by mewla, the daemon's executable. Some are the agent's
// other helpers (a person's own hook); when the turn has no recognized
// command at all, they can also mean the agent changed how it runs commands
// and match no longer knows the form.
func unrecognizedTurnChildren(agentPID int, processes map[int]processInfo, since time.Time, match func(string) (string, bool), startOf func(int) (time.Time, bool), mewla string) []turnUnrecognized {
	floor := since.Add(-turnCommandStartSlack)
	var found []turnUnrecognized
	for _, child := range processes {
		if child.ppid != agentPID || child.pid == agentPID || child.pid <= 0 {
			continue
		}
		if processGroupsAreReal && child.pgid != child.pid {
			continue
		}
		if _, ok := match(child.args); ok {
			continue
		}
		if mewla != "" && strings.Contains(child.args, mewla) {
			continue
		}
		start, ok := startOf(child.pid)
		if !ok || start.Before(floor) {
			continue
		}
		found = append(found, turnUnrecognized{process: turnCommandProcess{pid: child.pid, start: start}, args: child.args})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].process.pid < found[j].process.pid })
	return found
}

// turnUnrecognized is one process unrecognizedTurnChildren found.
type turnUnrecognized struct {
	process turnCommandProcess
	args    string
}

// turnUnrecognizedShown bounds how much of a command line the warning logs.
const turnUnrecognizedShown = 200

// reportUnrecognized waits out the grace a command would get, so a hook that
// was only passing through is gone, and then, for what is still running,
// logs a warning naming the agent's version and tells the chat that so many
// commands may still be running: a Stop that recognizes nothing is never
// silent.
func reportUnrecognized(unknown []turnUnrecognized, scope TurnCommandScope, alive func(turnCommandProcess) bool, sleep func(time.Duration)) {
	sleep(turnCommandGrace)
	var left []string
	for _, process := range unknown {
		if alive(process.process) {
			args := process.args
			if runes := []rune(args); len(runes) > turnUnrecognizedShown {
				args = string(runes[:turnUnrecognizedShown]) + "…"
			}
			left = append(left, fmt.Sprintf("pid %d: %q", process.process.pid, args))
		}
	}
	if len(left) == 0 {
		return
	}
	agent := "the agent"
	if scope.Agent != nil {
		if named := strings.TrimSpace(scope.Agent()); named != "" {
			agent = named
		}
	}
	log.Printf("WARN stop: %s has %d child process(es) in their own process group, started this turn, that are not commands Mewla recognizes, and none that are; it may have changed how it runs commands, so Stop left them running: %s", agent, len(left), strings.Join(left, "; "))
	if scope.Report != nil {
		stopped := make([]agentevents.StoppedCommand, len(left))
		for i := range stopped {
			stopped[i] = agentevents.StoppedCommand{Unrecognized: true}
		}
		scope.Report(stopped)
	}
}

// commandGroupMembers are the processes a command's group holds: those with
// its process group where groups are real, and its process tree elsewhere.
func commandGroupMembers(leader processInfo, processes map[int]processInfo) []processInfo {
	members := []processInfo{leader}
	if processGroupsAreReal {
		for _, process := range processes {
			if process.pgid == leader.pid && process.pid != leader.pid {
				members = append(members, process)
			}
		}
		return members
	}
	return append(members, descendantProcesses(leader.pid, processes)...)
}

// stopTurnCommands ends the commands the interrupted turn started under
// agentPID: SIGTERM to each group now, SIGKILL to what is left after
// turnCommandGrace, then one report of what ended.
func (w *Watcher) stopTurnCommands(agentPID int, scope TurnCommandScope) {
	processes, err := snapshotProcessTableNear([]int{agentPID})
	if err != nil {
		log.Printf("stop: cannot read the process table to end the turn's commands: %v", err)
		return
	}
	groups := turnCommandGroups(agentPID, processes, scope.Since, scope.Match, preciseProcessStart)
	if len(groups) == 0 {
		if unknown := unrecognizedTurnChildren(agentPID, processes, scope.Since, scope.Match, preciseProcessStart, ExecutablePath()); len(unknown) > 0 {
			go reportUnrecognized(unknown, scope, processStillThere, time.Sleep)
		}
		return
	}
	for i := range groups {
		if err := signalCommandGroup(groups[i], false); err != nil {
			groups[i].refused = err.Error()
			log.Printf("stop: cannot end %q (pid %d): %v", groups[i].command, groups[i].leader.pid, err)
		}
	}
	go func() {
		stopped := settleTurnCommands(groups, commandGroupAlive, signalCommandGroup, time.Sleep)
		if scope.Report != nil {
			scope.Report(stopped)
		}
	}()
}

// settleTurnCommands waits for the signalled groups to go, kills what is left
// after the grace, and says how each one ended.
func settleTurnCommands(groups []turnCommandGroup, alive func(turnCommandGroup) bool, signal func(turnCommandGroup, bool) error, sleep func(time.Duration)) []agentevents.StoppedCommand {
	waitGone := func(window time.Duration) {
		for waited := time.Duration(0); waited < window; waited += turnCommandPoll {
			running := false
			for _, group := range groups {
				if group.refused == "" && alive(group) {
					running = true
					break
				}
			}
			if !running {
				return
			}
			sleep(turnCommandPoll)
		}
	}
	waitGone(turnCommandGrace)
	killed := false
	for i, group := range groups {
		if group.refused == "" && alive(group) {
			if err := signal(group, true); err != nil {
				groups[i].refused = err.Error()
			}
			killed = true
		}
	}
	if killed {
		waitGone(turnCommandSettle)
	}
	stopped := make([]agentevents.StoppedCommand, 0, len(groups))
	for _, group := range groups {
		result := agentevents.StoppedCommand{Command: group.command}
		switch {
		case group.refused != "":
			result.Reason = "the system refused to end it: " + group.refused
		case alive(group):
			result.Reason = "it was still running after SIGKILL"
		default:
			result.Ended = true
		}
		stopped = append(stopped, result)
	}
	return stopped
}

// processStillThere reports whether p is still the process it was: a pid
// whose start moved is another process.
func processStillThere(p turnCommandProcess) bool {
	start, ok := preciseProcessStart(p.pid)
	if !ok {
		return false
	}
	diff := start.Sub(p.start)
	return diff > -time.Second && diff < time.Second
}
