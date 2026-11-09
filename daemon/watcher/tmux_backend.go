package watcher

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
)

// tmuxBackend runs Sessions as panes on the one tmux server selected at
// daemon startup. A Session id is the pane's immutable %id; labels and the
// input receipt ledger are pane-local @mewla_* options, so they live exactly
// as long as the pane.
type tmuxBackend struct {
	w *Watcher
}

var _ sessionBackend = (*tmuxBackend)(nil)

func (b *tmuxBackend) socket() string { return b.w.socketPathFor("") }

func (b *tmuxBackend) validID(id string) bool { return isPaneID(id) }

func (b *tmuxBackend) observedGeneration(id string) string {
	if !isPaneID(id) {
		return ""
	}
	return sessionInputPaneGeneration(id)
}

func (b *tmuxBackend) listOwned() ([]sessionPane, error) { return b.w.listTmuxPanes() }

func (b *tmuxBackend) listServices() ([]servicePane, error) {
	socket := b.socket()
	onSocket, err := listServicePanesOn(socket)
	if err != nil {
		if isNoTmuxServerError(err) {
			return nil, nil
		}
		return nil, err
	}
	panes := make([]servicePane, 0, len(onSocket))
	owned := make(map[string]bool)
	checked := make(map[string]bool)
	for _, pane := range onSocket {
		if !checked[pane.target] {
			present, targetOwned, probeErr := probeTmuxTargetOwnership(socket, pane.target)
			if probeErr != nil {
				return nil, probeErr
			}
			checked[pane.target] = true
			owned[pane.target] = present && targetOwned
		}
		if owned[pane.target] {
			panes = append(panes, pane)
		}
	}
	return panes, nil
}

func (b *tmuxBackend) probeOwnership(id string) (bool, bool, error) {
	return probeTmuxTargetOwnership(b.socket(), id)
}

func (b *tmuxBackend) resolveAbsence(id string, sawInventory bool) (bool, error) {
	socket := b.socket()
	present, owned, err := probeTmuxTargetOwnershipOn(socket, id, true)
	if err != nil && errors.Is(err, ErrOwnershipProbeUnavailable) && !sawInventory && tmuxSocketUnreachable(socket) {
		// This process never saw a tmux server and nothing listens on the
		// selected socket now: a Session recorded by an earlier run (a reboot, a
		// restart after tmux exited) is gone. A server that vanishes after being
		// seen stays Unknown.
		return true, nil
	}
	return !present || !owned, err
}

func (b *tmuxBackend) processRoot(target string) (int, error) {
	out, err := tmuxCommand(b.socket(), "display-message", "-p", "-t", target, "#{pane_dead}\t#{pane_pid}").Output()
	if err != nil {
		return 0, fmt.Errorf("tmux display-message for %s: %w%s", target, err, commandStderr(err))
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 2 {
		return 0, fmt.Errorf("tmux display-message for %s returned %q, want pane_dead<TAB>pane_pid", target, truncateDiagnostic(string(out)))
	}
	if fields[0] == "1" {
		return 0, fmt.Errorf("tmux pane %s is dead", target)
	}
	panePID, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil || panePID <= 0 {
		return 0, fmt.Errorf("tmux pane %s reported pane_pid %q", target, fields[1])
	}
	return panePID, nil
}

func (b *tmuxBackend) currentPath(id string) (string, error) {
	return currentPathForTarget(b.socket(), id)
}

func (b *tmuxBackend) delegatedResource(id string) (bool, string) {
	return tmuxDelegatedResource(b.socket(), id)
}

func (b *tmuxBackend) captureState(id string) (string, bool, int) {
	return capturePaneContentOn(b.socket(), id)
}

func (b *tmuxBackend) captureRecent(id string) (string, error) {
	out, err := tmuxCommand(b.socket(), "capture-pane", "-t", id, "-p", "-S", "-200").Output()
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}
	return string(out), nil
}

func (b *tmuxBackend) captureVisible(id string, styled bool) (string, error) {
	args := []string{"capture-pane", "-p", "-t", id}
	if styled {
		args = []string{"capture-pane", "-p", "-e", "-t", id}
	}
	out, err := tmuxCommand(b.socket(), args...).Output()
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}
	return string(out), nil
}

func (b *tmuxBackend) captureDialog(id string) (string, classifier.CaptureInfo, error) {
	return capturePaneDialog(b.socket(), id)
}

func (b *tmuxBackend) sendKeys(id string, keys ...string) error {
	return tmuxCommand(b.socket(), append([]string{"send-keys", "-t", id}, keys...)...).Run()
}

func (b *tmuxBackend) repeatKey(id, key string, count int) error {
	return tmuxCommand(b.socket(), "send-keys", "-t", id, "-N", fmt.Sprint(count), key).Run()
}

func (b *tmuxBackend) sendLiteral(id, text string) error {
	if out, err := tmuxCommand(b.socket(), "send-keys", "-l", "-t", id, "--", text).CombinedOutput(); err != nil {
		return fmt.Errorf("send literal tmux input: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

// sendBytes types exact bytes. -H avoids tmux argument parsing, which drops a
// trailing ';' and treats a leading '-' as a flag.
func (b *tmuxBackend) sendBytes(id string, data []byte) error {
	args := []string{"send-keys", "-t", id, "-H"}
	for _, c := range data {
		args = append(args, hex.EncodeToString([]byte{c}))
	}
	return tmuxCommand(b.socket(), args...).Run()
}

// place puts a new pane in the tmux session of an explicitly Mewla-owned
// target when one is named. Otherwise the first session containing a
// Mewla-owned pane is used, or a new detached session is created. Ambient user
// sessions are never joined or adopted.
func (b *tmuxBackend) place(near string, detached bool, name string) (backendPlacement, error) {
	socket := b.socket()
	if near != "" {
		present, owned, err := probeTmuxTargetOwnership(socket, near)
		if err != nil {
			return backendPlacement{}, err
		}
		if !present {
			return backendPlacement{}, fmt.Errorf("preferred tmux target %q is missing", near)
		}
		if !owned {
			return backendPlacement{}, fmt.Errorf("%w: %s", ErrUnownedTmuxTarget, near)
		}
	}
	sessionName := ""
	if near != "" {
		out, err := tmuxCommand(socket, "display-message", "-p", "-t", near, "#{session_name}").Output()
		if err != nil {
			return backendPlacement{}, err
		}
		sessionName = strings.TrimSpace(string(out))
	}
	newGroup := detached
	if sessionName == "" {
		if !newGroup {
			sessions, err := listTmuxSessionsOn(socket)
			if err != nil {
				if !isNoTmuxServerError(err) {
					return backendPlacement{}, err
				}
			}
			if len(sessions) > 0 {
				sessionName = sessions[0]
			} else {
				newGroup = true
			}
		}
		if sessionName == "" {
			sessionName = newTmuxSessionName(name)
		}
	}
	return backendPlacement{group: sessionName, newGroup: newGroup}, nil
}

// create allocates the immutable pane first, then launches the command in it
// with respawn-pane: respawn-pane -e is pane-local, while new-session -e would
// contaminate the environment of later user panes.
func (b *tmuxBackend) create(at backendPlacement, spec backendCreateSpec) (string, error) {
	socket := b.socket()
	var args []string
	if at.newGroup {
		args = buildNewSessionArgs(at.group, spec.cwd, spec.name, "sleep 86400")
	} else {
		args = buildNewWindowArgs(at.group, spec.cwd, spec.name, "sleep 86400")
	}
	out, err := tmuxCommand(socket, args...).Output()
	if err != nil {
		return "", fmt.Errorf("allocate Worker pane: %w", err)
	}
	target := strings.TrimSpace(string(out))
	if !isPaneID(target) {
		return "", fmt.Errorf("tmux returned invalid pane identity %q", target)
	}
	cleanup := func(cause error) (string, error) {
		if err := tmuxCommand(socket, "kill-pane", "-t", target).Run(); err != nil && !tmuxTargetGone(socket, target) {
			return "", fmt.Errorf("%v; remove allocated pane: %w", cause, err)
		}
		return "", cause
	}
	// The pane's first output is its origin marker: tmux evicts the oldest
	// lines first, so a capture that still holds the marker holds every line
	// after it. The nonce is kept in the pane's own option, which outlives a
	// daemon restart.
	nonce, err := newOriginNonce()
	if err != nil {
		return cleanup(fmt.Errorf("origin marker: %w", err))
	}
	if err := setTmuxPaneUserOption(socket, target, strings.TrimPrefix(originOption, "@"), nonce); err != nil {
		return cleanup(fmt.Errorf("origin marker: %w", err))
	}
	// A tmux server started inside a session host Session holds that
	// Session's MEWLA_SESSION_ID in its global environment; blank it so the
	// pane's Worker id comes from TMUX_PANE.
	args = []string{"respawn-pane", "-k", "-t", target, "-e", "MEWLA_SESSION_ID="}
	for _, entry := range spec.env {
		args = append(args, "-e", entry)
	}
	args = append(args, originMarkerCommand(nonce)+spec.command)
	if err := tmuxCommand(socket, args...).Run(); err != nil {
		return cleanup(fmt.Errorf("launch Worker pane: %w", err))
	}
	if err := markCreatedSession(socket, target, spec.labels); err != nil {
		return cleanup(fmt.Errorf("mark owned tmux pane: %w", err))
	}
	return target, nil
}

// kill removes the pane. A failed kill is "already gone" only when a fresh
// listing says so; any other failure leaves the pane possibly live.
func (b *tmuxBackend) kill(id string) error {
	socket := b.socket()
	out, killErr := tmuxCommand(socket, "kill-pane", "-t", id).CombinedOutput()
	if killErr != nil && !tmuxTargetGone(socket, id) {
		return fmt.Errorf("kill tmux pane: %w: %s", killErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func (b *tmuxBackend) inputIO() sessionInputIO {
	return realSessionInputIO{socketFor: b.w.socketPathFor}
}
