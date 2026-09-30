package terminal

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const historyLineLimit = 6000
const historyByteLimit = 4 << 20

// History is a bounded, styled snapshot of the attached view's active pane.
// It never enters copy-mode or changes the live PTY screen.
type History struct {
	PaneID    string `json:"pane_id"`
	Cols      int    `json:"cols"`
	Total     int    `json:"total"`
	Alternate bool   `json:"alternate"`
	ANSI      string `json:"ansi"`
}

type historyProvider interface{ History() (History, error) }

func (m *Manager) History(ownerID, sessionID string) (History, error) {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return History{}, err
	}
	provider, ok := ms.session.(historyProvider)
	if !ok {
		return History{}, fmt.Errorf("session does not support pane history")
	}
	// Capture is read-only. Do not hold the input/scroll serialization lock
	// while waiting for a tmux process: typing must not wait behind history.
	return provider.History()
}

// boundedHistoryBuffer stops a pathological styled capture before it can grow
// daemon memory without bound. No partial/truncated escape stream is returned.
type boundedHistoryBuffer struct{ bytes.Buffer }

func (b *boundedHistoryBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > historyByteLimit {
		return 0, fmt.Errorf("pane history exceeds 4 MiB")
	}
	return b.Buffer.Write(p)
}

func (s *tmuxSession) History() (History, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return History{}, fmt.Errorf("tmux session is closed")
	}
	target, socket := s.interactiveTargetLocked(), s.socket
	s.mu.Unlock()
	if target == "" {
		return History{}, fmt.Errorf("tmux session has no interactive target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// All commands run in one tmux command queue; a pane switch cannot select a
	// different target between metadata and capture. Physical rows retain the
	// exact pane width; capture -e preserves attributes for Ghostty to interpret.
	cmd := tmuxCommandContext(ctx, socket,
		"display-message", "-p", "-t", target,
		"#{pane_id}\t#{pane_width}\t#{history_size}\t#{alternate_on}",
		";", "if-shell", "-F", "-t", target,
		"#{&&:#{history_size},#{==:#{alternate_on},0}}",
		"capture-pane -p -e -N -t "+strconv.Quote(target)+" -S -6000 -E -1", "")
	out := &boundedHistoryBuffer{}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return History{}, fmt.Errorf("capture pane history: %w", err)
	}
	return parseHistory(out.String())
}

func parseHistory(raw string) (History, error) {
	header, ansi, ok := strings.Cut(raw, "\n")
	fields := strings.Split(header, "\t")
	if !ok || len(fields) != 4 {
		return History{}, fmt.Errorf("invalid pane history metadata")
	}
	cols, e1 := strconv.Atoi(fields[1])
	total, e2 := strconv.Atoi(fields[2])
	if e1 != nil || e2 != nil || cols < 1 || cols > 1000 || total < 0 {
		return History{}, fmt.Errorf("invalid pane history dimensions")
	}
	if len(ansi) > historyByteLimit || strings.Count(ansi, "\n") > historyLineLimit {
		return History{}, fmt.Errorf("pane history exceeds snapshot limit")
	}
	return History{PaneID: fields[0], Cols: cols, Total: total, Alternate: fields[3] == "1", ANSI: ansi}, nil
}
