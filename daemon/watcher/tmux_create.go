package watcher

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// newOriginNonce is the private part of a pane's origin marker.
func newOriginNonce() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// originMarkerCommand prints the marker, concealed (SGR 8), as the very first
// output of the pane, before the login shell and anything its startup prints.
func originMarkerCommand(nonce string) string {
	return "printf '\\033[8m%s\\033[0m\\n' " + shellquote.Quote(classifier.OriginMarkerLine(nonce)) + "; "
}

func buildNewWindowArgs(sessionName, cwd, name, shellCommand string) []string {
	args := []string{
		"new-window",
		"-P",
		"-F",
		"#{pane_id}",
		"-t",
		sessionName,
	}
	args = appendTmuxCreateOptions(args, cwd, name)
	if shellCommand != "" {
		args = append(args, shellCommand)
	}
	return args
}

func buildNewSessionArgs(sessionName, cwd, name, shellCommand string) []string {
	args := []string{
		"new-session",
		"-d",
		"-P",
		"-F",
		"#{pane_id}",
		"-s",
		sessionName,
	}
	args = appendTmuxCreateOptions(args, cwd, name)
	if shellCommand != "" {
		args = append(args, shellCommand)
	}
	return args
}

func appendTmuxCreateOptions(args []string, cwd, name string) []string {
	if name = strings.TrimSpace(name); name != "" {
		args = append(args, "-n", name)
	}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	return args
}

func newTmuxSessionName(name string) string {
	base := strings.ToLower(name)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-_")
	if base == "" {
		base = "worker"
	}
	return fmt.Sprintf("mewla-worker-%s-%d", base, time.Now().UnixNano())
}

// markCreatedSession records a created Session's labels as pane options, so
// they live exactly as long as the pane and survive a daemon restart.
func markCreatedSession(socket, target string, labels sessionLabels) error {
	if err := setTmuxPaneUserOption(socket, target, "mewla_worker_created", "1"); err != nil {
		return err
	}
	if labels.piSession != "" {
		if err := setTmuxPaneUserOption(socket, target, "mewla_worker_pi_session", labels.piSession); err != nil {
			return err
		}
	}
	if id := strings.TrimSpace(labels.executorID); id != "" {
		if err := setTmuxPaneUserOption(socket, target, "mewla_worker_executor", id); err != nil {
			return err
		}
	}
	if control := strings.TrimSpace(labels.agentControl); control != "" {
		if err := setTmuxPaneUserOption(socket, target, "mewla_worker_agent_control", control); err != nil {
			return err
		}
	}
	if labels.hidden {
		if err := setTmuxPaneUserOption(socket, target, "mewla_worker_hidden", "1"); err != nil {
			return err
		}
	}
	if labels.delegated {
		if err := setTmuxPaneUserOption(socket, target, "mewla_worker_delegated", "1"); err != nil {
			return err
		}
		if labels.resourceUnit != "" || labels.resourceOwner != "" {
			if err := setTmuxPaneUserOption(socket, target, "mewla_worker_resource_unit", labels.resourceUnit); err != nil {
				return err
			}
			if err := setTmuxPaneUserOption(socket, target, "mewla_worker_resource_owner", labels.resourceOwner); err != nil {
				return err
			}
		}
	}
	return nil
}

func setTmuxPaneUserOption(socket, target, key, value string) error {
	if !isPaneID(target) {
		return fmt.Errorf("invalid pane identity %q", target)
	}
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if target == "" || key == "" || value == "" {
		return fmt.Errorf("tmux pane option target, key, and value are required")
	}
	out, err := tmuxCommand(socket, "set-option", "-p", "-t", target, "@"+key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set @%s on %s: %w: %s", key, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tmuxPaneUserOption(socket, target, key string) (string, error) {
	if !isPaneID(target) {
		return "", fmt.Errorf("invalid pane identity %q", target)
	}
	target = strings.TrimSpace(target)
	key = strings.TrimSpace(key)
	if target == "" || key == "" {
		return "", fmt.Errorf("tmux pane option target and key are required")
	}
	out, err := tmuxCommand(
		socket,
		"show-options",
		"-p",
		"-qv",
		"-t",
		target,
		"@"+key,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("show @%s on %s: %w: %s", key, target, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
