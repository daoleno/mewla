package watcher

import (
	"fmt"
	"strconv"
	"strings"
)

const ownershipOption = "@mewla_worker_created"

// ownedPaneSet reports which of the given panes carry Mewla's pane-local
// ownership marker, in one tmux invocation instead of one list-panes plus one
// show-options per pane. Each pane's value is read with `show-options -p`
// without -A, so a window, session or global option of the same name is never
// inherited. The panes were just inventoried; if one vanished since, tmux
// fails the whole call and the caller falls back to probing pane by pane.
// The pane marker is a #{pane_id} format, never a literal %id: tmux runs a
// display-message template through strftime, which rewrites %3 and %5.
func ownedPaneSet(socket string, targets []string) (map[string]bool, error) {
	owned := make(map[string]bool, len(targets))
	if len(targets) == 0 {
		return owned, nil
	}
	args := make([]string, 0, len(targets)*9)
	for i, target := range targets {
		if !isPaneID(target) {
			return nil, fmt.Errorf("not a pane id: %q", target)
		}
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, "display-message", "-p", "-t", target, "=#{pane_id}", ";", "show-options", "-pqv", "-t", target, ownershipOption)
	}
	out, err := tmuxCommand(socket, args...).Output()
	if err != nil {
		return nil, err
	}
	current := ""
	seen := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "=%") {
			current = strings.TrimPrefix(line, "=")
			seen++
			continue
		}
		if current != "" && tmuxBoolOption(line) {
			owned[current] = true
		}
	}
	if seen != len(targets) {
		return nil, fmt.Errorf("tmux answered %d of %d panes", seen, len(targets))
	}
	return owned, nil
}

// paneStateMarker separates a captured screen from the state line that
// follows it in the same tmux invocation. A control byte keeps it from
// appearing in terminal text.
const paneStateMarker = "\x1fmewla-pane-state\x1f"

// capturePaneWithState captures a pane and reads its liveness in one tmux
// invocation. The screen is everything before the final marker.
func capturePaneWithState(socket, target string) (screen string, alive bool, deadStatus int, err error) {
	out, err := tmuxCommand(socket,
		"capture-pane", "-t", target, "-p", "-S", "-200", ";",
		"display-message", "-p", "-t", target, paneStateMarker+"#{pane_dead}\t#{pane_dead_status}",
	).Output()
	if err != nil {
		return "", false, -1, err
	}
	text := string(out)
	cut := strings.LastIndex(text, paneStateMarker)
	if cut < 0 {
		return "", false, -1, fmt.Errorf("tmux returned no pane state")
	}
	alive, deadStatus = true, -1
	fields := strings.Split(strings.TrimSpace(text[cut+len(paneStateMarker):]), "\t")
	if len(fields) >= 1 && fields[0] == "1" {
		alive = false
		if len(fields) >= 2 {
			if status, parseErr := strconv.Atoi(strings.TrimSpace(fields[1])); parseErr == nil {
				deadStatus = status
			}
		}
	}
	return text[:cut], alive, deadStatus, nil
}

// ownedPaneSetFor is ownedPaneSet over an inventory. It returns nil when the
// batch could not answer, and the caller then probes each pane on its own.
func ownedPaneSetFor(socket string, panes []tmuxPane) map[string]bool {
	targets := make([]string, 0, len(panes))
	for _, pane := range panes {
		targets = append(targets, pane.target)
	}
	owned, err := ownedPaneSet(socket, targets)
	if err != nil {
		return nil
	}
	return owned
}
