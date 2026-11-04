package watcher

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
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
// follows it in the same tmux invocation. It is printable text: tmux versions
// differ in how they escape control bytes in display-message output. The state
// line is found by its last occurrence, so the same text on the screen cannot
// be mistaken for it.
const paneStateMarker = "@@mewla-pane-state@@"

// capturePaneWithState captures a pane and reads its liveness in one tmux
// invocation. The screen is everything before the final marker.
func capturePaneWithState(socket, target string) (screen string, alive bool, deadStatus int, err error) {
	out, err := tmuxCommand(socket,
		"capture-pane", "-t", target, "-p", "-S", "-200", ";",
		"display-message", "-p", "-t", target, paneStateMarker+"#{pane_dead} #{pane_dead_status}",
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
	fields := strings.Fields(text[cut+len(paneStateMarker):])
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

// originOption is the pane-local option holding the nonce of the pane's
// origin marker (classifier.OriginMarkerLine), set when Mewla launches a Worker.
const originOption = "@mewla_origin"

// capturePaneDialog captures a pane for identifying a dialog that asks to run
// something: the whole scrollback (-S -), with the spaces a program wrote at
// the end of a line kept and terminal-wrapped lines joined (-J). In the same
// tmux invocation it reads the pane's own origin nonce, so the capture says
// which marker line, if it still holds that line, proves it starts no later
// than the pane's first output. How full the scrollback is proves nothing
// (tmux trims in batches), so it is not read.
func capturePaneDialog(socket, target string) (string, classifier.CaptureInfo, error) {
	out, err := tmuxCommand(socket,
		"capture-pane", "-t", target, "-p", "-J", "-S", "-", ";",
		"display-message", "-p", "-t", target, paneStateMarker, ";",
		"show-options", "-pqv", "-t", target, originOption,
	).Output()
	if err != nil {
		return "", classifier.CaptureInfo{}, fmt.Errorf("capture pane: %w", err)
	}
	text := string(out)
	cut := strings.LastIndex(text, paneStateMarker)
	if cut < 0 {
		return "", classifier.CaptureInfo{}, fmt.Errorf("tmux returned no pane state")
	}
	info := classifier.CaptureInfo{Exact: true}
	if nonce := strings.TrimSpace(text[cut+len(paneStateMarker):]); nonce != "" {
		info.Origin = classifier.OriginMarkerLine(nonce)
	}
	return text[:cut], info, nil
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
