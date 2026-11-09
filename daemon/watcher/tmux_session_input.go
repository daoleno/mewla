package watcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// realSessionInputIO executes every tmux mutation on the one host server
// selected at daemon startup. socketFor returns that immutable server binding;
// nil keeps ordinary default-server semantics for test doubles.
type realSessionInputIO struct {
	socketFor func(sessionID string) string
}

func (io realSessionInputIO) socket(sessionID string) string {
	if io.socketFor != nil {
		return io.socketFor(sessionID)
	}
	return ""
}

func (io realSessionInputIO) pane(socket, sessionID string) sessionInputPane {
	if !isPaneID(sessionID) {
		return sessionInputPane{}
	}
	out, err := tmuxCommand(
		socket,
		"display-message",
		"-p",
		"-t",
		sessionID,
		// Immutable pane lifetime only. App Terminal opens a disposable
		// link-window view session; display-message -t %pane_id can therefore
		// report a different session_id/session_created than
		// display-message -t session:window for the same pane. pane_pid and
		// pane_start_command are launch/process metadata, not pane identity.
		// Live provider process identity (PID + start times) remains
		// fail-closed at every mutation boundary via guardTargetIdentity.
		"#{pane_dead}\t#{pane_id}",
	).Output()
	if err != nil {
		return sessionInputPane{}
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(fields) != 2 || fields[0] == "1" || strings.TrimSpace(fields[1]) != sessionID {
		return sessionInputPane{}
	}
	paneID := strings.TrimSpace(fields[1])
	return sessionInputPane{
		alive:      true,
		paneID:     paneID,
		generation: sessionInputPaneGeneration(paneID),
	}
}

// sessionInputPaneGeneration is the opaque digest of an immutable tmux pane
// lifetime id. It must not include session_id/session_created (linked App view
// sessions), window linkage metadata that is not pane-exclusive, pane_pid, or
// pane_start_command. Replaced panes get a new pane_id; respawned processes in
// the same pane are rejected by targetProcessIdentity instead.
func sessionInputPaneGeneration(paneID string) string {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(paneID))
	return fmt.Sprintf("%x", digest[:])
}

var sessionInputBufferSequence atomic.Uint64

// submitPaste loads the payload into a private tmux buffer and runs one
// target-bound command queue that clears the draft, pastes the buffer and
// submits it. A failed load or queue start left the pane untouched.
func (io realSessionInputIO) submitPaste(
	socket, paneID, payload string,
	adapter sessionInputProvider,
	beforeStart func() error,
) (bool, error) {
	return submitTmuxPaste(io, socket, paneID, payload, adapter, beforeStart)
}

// tmuxPasteIO is the buffer and queue surface submitTmuxPaste drives.
type tmuxPasteIO interface {
	loadBuffer(socket, buffer, payload string) error
	deleteBuffer(socket, buffer string)
	runQueue(socket string, args []string, beforeStart func() error) (started bool, err error)
}

func submitTmuxPaste(
	io tmuxPasteIO,
	socket, paneID, payload string,
	adapter sessionInputProvider,
	beforeStart func() error,
) (bool, error) {
	buffer := fmt.Sprintf("mewla-session-input-%d-%d", os.Getpid(), sessionInputBufferSequence.Add(1))
	if err := io.loadBuffer(socket, buffer, payload); err != nil {
		return false, err
	}
	defer io.deleteBuffer(socket, buffer)
	started, err := io.runQueue(socket, sessionInputSubmitQueue(paneID, buffer, adapter), beforeStart)
	if err != nil {
		if started {
			return true, fmt.Errorf("run target-bound tmux command queue: %w", err)
		}
		return false, fmt.Errorf("start target-bound tmux command queue: %w", err)
	}
	return true, nil
}

func (realSessionInputIO) loadBuffer(socket, buffer, payload string) error {
	command := tmuxCommand(socket, "load-buffer", "-b", buffer, "-")
	command.Stdin = strings.NewReader(payload)
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("load payload into tmux buffer: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func (realSessionInputIO) deleteBuffer(socket, buffer string) {
	_ = tmuxCommand(socket, "delete-buffer", "-b", buffer).Run()
}

func (realSessionInputIO) runQueue(
	socket string,
	args []string,
	beforeStart func() error,
) (bool, error) {
	command := tmuxCommand(socket, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	// This is the last pre-mutation operation: the target-bound tmux command
	// has already been constructed, but Start has not been called. The guard
	// re-proves the provider process lifetime and immutable pane generation.
	// It closes deterministic replacements during baseline/ledger work; it
	// does not pretend an external process can never race after this check.
	if beforeStart != nil {
		if err := beforeStart(); err != nil {
			return false, err
		}
	}
	if err := command.Start(); err != nil {
		return false, err
	}
	if err := command.Wait(); err != nil {
		return true, fmt.Errorf("%w%s", err, commandOutputSuffix(stderr.Bytes()))
	}
	return true, nil
}

func (io realSessionInputIO) receiptLedger(socket, target string) (sessionInputReceiptLedger, error) {
	value, err := tmuxPaneUserOption(socket, target, sessionInputReceiptOption)
	if err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return decodeSessionInputReceiptLedger(value)
}

func (io realSessionInputIO) writeReceiptLedger(socket, target string, ledger sessionInputReceiptLedger) error {
	if err := validateSessionInputReceiptLedger(ledger); err != nil {
		return err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("encode Session input receipt ledger: %w", err)
	}
	out, err := tmuxCommand(
		socket,
		"set-option",
		"-p",
		"-t",
		target,
		"@"+sessionInputReceiptOption,
		string(raw),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("write Session input receipt ledger: %w%s", err, commandOutputSuffix(out))
	}
	return nil
}

func sessionInputSubmitQueue(
	paneID string,
	buffer string,
	adapter sessionInputProvider,
) []string {
	args := []string{
		// Chat is authoritative over an unsent Terminal draft.
		"send-keys", "-t", paneID, "C-u",
	}
	if adapter.prepare > 0 {
		args = append(args,
			";", "run-shell", "sleep "+strconv.FormatFloat(adapter.prepare.Seconds(), 'f', 3, 64),
		)
	}
	if adapter.delegatedIntent {
		args = append(args, ";", "send-keys", "-l", "-t", paneID, DelegatedInputPrefix,
			";", "run-shell", "sleep 0.050")
	}
	args = append(args,
		// -r is part of the payload contract: without it tmux rewrites every
		// LF byte as CR, which terminal composers interpret as submit keys.
		";", "paste-buffer", "-r", "-p", "-b", buffer, "-t", paneID,
	)
	if adapter.settle > 0 {
		args = append(args,
			";", "run-shell", "sleep "+strconv.FormatFloat(adapter.settle.Seconds(), 'f', 3, 64),
		)
	}
	args = append(args, ";", "send-keys", "-t", paneID, adapter.submitKey)
	args = append(args, ";", "delete-buffer", "-b", buffer)
	return args
}

const sessionInputReceiptOption = "mewla_session_input_receipts"
