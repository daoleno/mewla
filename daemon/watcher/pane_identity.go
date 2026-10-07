package watcher

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/envcompat"
)

// isPaneID admits only tmux's immutable, server-local pane lifetime identifier.
// Names, windows and session-qualified targets are never execution identities.
func isPaneID(id string) bool {
	if len(id) < 2 || id[0] != '%' {
		return false
	}
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(id[1:], 10, 64)
	return err == nil
}

// CanonicalWorkerID accepts only the aliases pinned by startup migration. It
// never asks tmux to interpret an old window name or its current active pane.
func (w *Watcher) CanonicalWorkerID(id string) string {
	w.mu.RLock()
	pane := w.legacyWorkerIDs[id]
	socket := w.tmuxSocketPath
	w.mu.RUnlock()
	if pane != "" {
		legacy, err := tmuxPaneUserOption(socket, pane, "zen_worker_legacy_id")
		if err == nil && legacy == id {
			return pane
		}
	}
	return id
}

func (w *Watcher) SetLegacyWorkerIDs(aliases map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.legacyWorkerIDs = make(map[string]string, len(aliases))
	for old, pane := range aliases {
		w.legacyWorkerIDs[old] = pane
	}
}

// LegacyWorkerPanes pins legacy owned windows once at startup. Historical pane
// generation evidence wins over focus; without it only a single-pane window
// can be adopted. An ambiguous split is left unowned, never guessed.
// persist is called before changing tmux metadata so interrupted migration can
// always resume with the same pane and can never adopt a later replacement.
func (w *Watcher) MigrateLegacyWorkerPanes(aliases map[string]string, generations map[string][]string, persist func(map[string]string) error) error {
	socket := w.socketPathFor("")
	for old, pane := range aliases {
		if !isPaneID(pane) {
			return fmt.Errorf("invalid migrated pane for %s", old)
		}
	}
	out, err := tmuxCommand(socket, "list-panes", "-a", "-F", "#{session_name}:#{window_id}\t#{pane_id}").CombinedOutput()
	if err != nil {
		if isNoTmuxServerError(fmt.Errorf("%v: %s", err, out)) {
			return nil
		}
		return fmt.Errorf("inventory legacy Worker panes: %w: %s", err, out)
	}
	windows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || !isPaneID(fields[1]) || strings.HasPrefix(fields[0], "zen-view-") {
			continue
		}
		windows[fields[0]] = append(windows[fields[0]], fields[1])
	}
	keys := []string{"zen_worker_hidden", "zen_worker_delegated", "zen_worker_resource_unit", "zen_worker_resource_owner", "zen_worker_pi_session", sessionInputReceiptOption, "zen_worker_created"}
	for old, panes := range windows {
		// Use the exact inventory's immutable window ID, never a session:name target.
		_, window, _ := strings.Cut(old, ":")
		raw, err := tmuxCommand(socket, "show-options", "-wqv", "-t", window, "@zen_worker_created").Output()
		if err != nil {
			return err
		}
		if !tmuxBoolOption(string(raw)) {
			continue
		}
		pane := aliases[old]
		if pane == "" {
			for _, candidate := range panes {
				for _, generation := range generations[old] {
					if generation == sessionInputPaneGeneration(candidate) {
						if pane != "" && pane != candidate {
							return fmt.Errorf("legacy Worker %s has conflicting pane generations", old)
						}
						pane = candidate
					}
				}
			}
			if pane == "" && len(generations[old]) == 0 && len(panes) == 1 {
				pane = panes[0]
			}
			if pane == "" {
				return fmt.Errorf("cannot prove owned pane for legacy Worker %s; refusing active-pane migration", old)
			}
			aliases[old] = pane
			if err := persist(aliases); err != nil {
				return err
			}
		}
		present := false
		for _, candidate := range panes {
			present = present || candidate == pane
		}
		// A previously pinned pane that disappeared stays gone. Its sibling cannot
		// inherit the legacy window's marker even after a partially completed load.
		if present {
			if err := setTmuxPaneUserOption(socket, pane, "zen_worker_legacy_id", old); err != nil {
				return err
			}
			for _, key := range keys {
				value, err := tmuxCommand(socket, "show-options", "-wqv", "-t", window, "@"+key).Output()
				if err != nil {
					return err
				}
				if v := strings.TrimSpace(string(value)); v != "" {
					if err := setTmuxPaneUserOption(socket, pane, key, v); err != nil {
						return err
					}
				}
			}
		}
		session, _, _ := strings.Cut(old, ":")
		if strings.HasPrefix(session, "zen-worker-") {
			env, err := tmuxCommand(socket, "show-environment", "-t", "="+session).Output()
			if err != nil {
				return err
			}
			for _, entry := range strings.Split(string(env), "\n") {
				key, _, ok := strings.Cut(entry, "=")
				if !ok {
					continue
				}
				if strings.HasPrefix(key, envcompat.Prefix) || strings.HasPrefix(key, envcompat.LegacyPrefix) || key == "TMUX_TMPDIR" || key == "TMPDIR" || key == "TMP" || key == "TEMP" || key == "CODEX_HOME" || key == "CLAUDE_CONFIG_DIR" || key == "PI_CODING_AGENT_DIR" {
					if out, err := tmuxCommand(socket, "set-environment", "-u", "-t", "="+session, key).CombinedOutput(); err != nil {
						return fmt.Errorf("remove legacy session environment: %w: %s", err, out)
					}
				}
			}
		}
		for _, key := range keys {
			if out, err := tmuxCommand(socket, "set-option", "-wu", "-t", window, "@"+key).CombinedOutput(); err != nil {
				return fmt.Errorf("remove legacy option: %w: %s", err, out)
			}
		}

	}
	return nil
}
