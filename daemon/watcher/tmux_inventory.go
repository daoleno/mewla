package watcher

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
)

// listTmuxPanes inventories only explicitly Mewla-owned panes on the one
// caller-visible server selected at startup. Ambient user panes are read only
// as part of tmux's formatted listing and are discarded by the durable
// @mewla_worker_created marker before they can enter discovery or reconciliation.
// A missing/unreadable server is Unknown, never a successful empty inventory.
// Only a successful observation may drive removal reconciliation.
func (w *Watcher) listTmuxPanes() ([]sessionPane, error) {
	if w == nil {
		return nil, nil
	}
	w.mu.RLock()
	socket := w.tmuxSocketPath
	w.mu.RUnlock()
	onSocket, err := listTmuxPanesOn(socket)
	if err != nil {
		return nil, err
	}
	w.screens.inventoryOf(onSocket, isPaneID)
	windows := make([]sessionPane, 0, len(onSocket))
	markers, ask := w.screens.ownership(onSocket)
	if len(ask) > 0 {
		if asked := ownedPaneSetFor(socket, ask); asked != nil {
			w.screens.learnOwnership(ask, asked)
			for target := range asked {
				markers[target] = true
			}
		} else {
			markers = nil
		}
	}
	for _, win := range onSocket {
		if markers != nil {
			if markers[win.target] {
				windows = append(windows, win)
			}
			continue
		}
		present, owned, probeErr := probeTmuxTargetOwnershipOn(socket, win.target, true)
		if probeErr != nil {
			return nil, probeErr
		}
		if present && owned {
			windows = append(windows, win)
		}
	}
	return windows, nil
}

// malformedPaneInventoryLogged keeps one diagnostic per daemon run for a pane
// inventory whose separators did not survive tmux output.
var malformedPaneInventoryLogged atomic.Bool

func listTmuxPanesOn(socket string) ([]sessionPane, error) {
	cmd := tmuxCommand(socket, "list-panes", "-a", "-F", "#{pane_id}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{@mewla_worker_hidden}\t#{@mewla_worker_delegated}\t#{@mewla_worker_resource_unit}\t#{@mewla_worker_pi_session}\t#{@mewla_worker_executor}\t#{@mewla_worker_agent_control}\t#{window_activity}\t#{pane_dead}\t#{session_name}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, tmuxFailure(socket, "list-panes", err, out)
	}
	var windows []sessionPane
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 14)
		if len(parts) != 14 && malformedPaneInventoryLogged.CompareAndSwap(false, true) {
			log.Printf("watcher: tmux list-panes line has %d of 14 tab-separated fields (non-UTF-8 tmux client?): %q", len(parts), truncateDiagnostic(line))
		}
		target := parts[0]
		// Terminal views link an owned window but are not execution Sessions.
		sessionName := ""
		if len(parts) >= 14 {
			sessionName = parts[13]
		}
		if strings.HasPrefix(sessionName, "mewla-view-") || seen[target] {
			continue
		}
		seen[target] = true
		name := target
		if len(parts) >= 2 {
			name = parts[1]
		}
		cwd := ""
		if len(parts) >= 3 {
			cwd = strings.TrimSpace(parts[2])
		}
		command := ""
		if len(parts) >= 4 {
			command = strings.TrimSpace(parts[3])
		}
		panePID := 0
		if len(parts) >= 5 {
			panePID, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		hidden := false
		if len(parts) >= 6 {
			hidden = tmuxBoolOption(parts[5])
		}
		delegated := false
		if len(parts) >= 7 {
			delegated = tmuxBoolOption(parts[6])
		}
		resourceUnit := ""
		if len(parts) >= 8 {
			resourceUnit = strings.TrimSpace(parts[7])
		}
		piSessionBinding := ""
		if len(parts) >= 9 {
			piSessionBinding = strings.TrimSpace(parts[8])
		}
		executorID := ""
		if len(parts) >= 10 {
			executorID = strings.TrimSpace(parts[9])
		}
		agentControl := ""
		if len(parts) >= 11 {
			agentControl = strings.TrimSpace(parts[10])
		}
		var activity int64
		dead := false
		if len(parts) >= 14 {
			activity, _ = strconv.ParseInt(strings.TrimSpace(parts[11]), 10, 64)
			dead = strings.TrimSpace(parts[12]) == "1"
		}
		windows = append(windows, sessionPane{
			activity: activity, dead: dead,
			target: target, name: name, cwd: cwd, command: command, sessionName: sessionName,
			piSessionBinding: piSessionBinding, executorID: executorID, agentControl: agentControl,
			panePID: panePID, hidden: hidden, delegated: delegated,
			resourceUnit: resourceUnit,
		})
	}
	return windows, nil
}

func tmuxBoolOption(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// probeTmuxTargetOwnership reads only pane-local ownership on the selected
// server. The exact pane inventory proves presence; show-options omits -A so
// ownership cannot be inherited from window, session, or global configuration.
// Missing targets are absence; unavailable inventory remains unknown.
func probeTmuxTargetOwnership(socket, target string) (present, owned bool, err error) {
	return probeTmuxTargetOwnershipOn(socket, target, false)
}

// requireServer distinguishes inventory evidence from an exact cleanup probe:
// cleanup may regard a missing server as absent, but discovery cannot publish
// absence when any step of its multi-command inventory lost server access.
func probeTmuxTargetOwnershipOn(socket, target string, requireServer bool) (present, owned bool, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, false, nil
	}
	present, err = tmuxTargetPresentOn(socket, target, requireServer)
	if err != nil || !present {
		return false, false, err
	}
	out, commandErr := tmuxCommand(
		socket,
		"show-options",
		"-p",
		"-qv",
		"-t",
		target,
		"@mewla_worker_created",
	).CombinedOutput()
	if commandErr != nil {
		// The pane was listed a moment ago. A list that no longer has it (or
		// no server) says it went away in between; otherwise ownership simply
		// could not be read, which is unknown, never "not owned".
		present, presenceErr := tmuxTargetPresentOn(socket, target, requireServer)
		if presenceErr != nil {
			return false, false, presenceErr
		}
		if !present {
			return false, false, nil
		}
		return false, false, fmt.Errorf("%w: %w", ErrOwnershipProbeUnavailable, tmuxFailure(socket, "show-options "+target, commandErr, out))
	}
	return true, tmuxBoolOption(string(out)), nil
}

func tmuxTargetPresentOn(socket, target string, requireServer bool) (bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, nil
	}
	out, commandErr := tmuxCommand(socket, "list-panes", "-a", "-F", "#{pane_id}").CombinedOutput()
	outText := strings.TrimSpace(string(out))
	if commandErr != nil {
		failure := tmuxFailure(socket, "list-panes", commandErr, out)
		if errors.Is(failure, ErrTmuxServerGone) {
			if requireServer {
				return false, fmt.Errorf("%w: selected tmux server became unavailable", ErrOwnershipProbeUnavailable)
			}
			return false, nil
		}
		return false, fmt.Errorf("probe tmux presence for %s: %w", target, failure)
	}
	if outText == "" {
		return false, fmt.Errorf("%w: tmux presence for %s returned no pane identity", ErrOwnershipProbeUnavailable, target)
	}
	for _, line := range strings.Split(outText, "\n") {
		if strings.TrimSpace(line) == target {
			return true, nil
		}
	}
	return false, nil
}

func capturePaneContentOn(socket, target string) (string, bool, int) {
	if !isPaneID(target) {
		return "", false, -1
	}
	if screen, alive, deadStatus, err := capturePaneWithState(socket, target); err == nil {
		return screen, alive, deadStatus
	}
	// A tmux that does not answer the combined call (older builds differ in
	// how they run a command list) is read with one call per fact.
	cmd := tmuxCommand(socket, "capture-pane", "-t", target, "-p", "-S", "-200")
	out, err := cmd.Output()
	if err != nil {
		return "", false, -1
	}
	alive, deadStatus := true, -1
	if aliveOut, err := tmuxCommand(socket, "display-message", "-p", "-t", target, "#{pane_dead}\t#{pane_dead_status}").Output(); err == nil {
		fields := strings.Split(strings.TrimSpace(string(aliveOut)), "\t")
		if len(fields) >= 1 && fields[0] == "1" {
			alive = false
			if len(fields) >= 2 {
				if status, parseErr := strconv.Atoi(strings.TrimSpace(fields[1])); parseErr == nil {
					deadStatus = status
				}
			}
		}
	}
	return string(out), alive, deadStatus
}

func tmuxDelegatedResource(socket, target string) (bool, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, ""
	}
	out, err := tmuxCommand(
		socket,
		"display-message",
		"-p",
		"-t",
		target,
		"#{@mewla_worker_delegated}\t#{@mewla_worker_resource_unit}",
	).Output()
	if err != nil {
		return false, ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 || !tmuxBoolOption(parts[0]) {
		return false, ""
	}
	return true, strings.TrimSpace(parts[1])
}

func listTmuxSessionsOn(socket string) ([]string, error) {
	windows, err := listTmuxPanesOn(socket)
	if err != nil {
		return nil, err
	}

	sessions := make([]string, 0)
	seen := make(map[string]bool)
	markers := ownedPaneSetFor(socket, windows)
	for _, win := range windows {
		if markers != nil {
			if !markers[win.target] {
				continue
			}
		} else {
			present, owned, probeErr := probeTmuxTargetOwnership(socket, win.target)
			if probeErr != nil {
				return nil, probeErr
			}
			if !present || !owned {
				continue
			}
		}
		sessionName := win.sessionName
		if sessionName == "" || seen[sessionName] {
			continue
		}
		seen[sessionName] = true
		sessions = append(sessions, sessionName)
	}
	return sessions, nil
}

func currentPathForTarget(socket, target string) (string, error) {
	out, err := tmuxCommand(socket, "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux current path: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func listServicePanesOn(socket string) ([]servicePane, error) {
	out, err := tmuxCommand(socket, "list-panes", "-a", "-F", "#{pane_id}\t#{pane_id}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{pane_active}").CombinedOutput()
	if err != nil {
		return nil, tmuxFailure(socket, "list-panes", err, out)
	}
	return parseServicePanes(string(out)), nil
}
