package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// QuickReplies are the tap-to-answer chips under Brain's latest message in
// the current chat thread. Any Brain question can use them: a chip sends its
// text as if the user typed it, so a typed answer always works too. Brain
// sets them after the message they answer (mewla brain replies); they go
// when the user sends anything, the thread changes or a day passes.
type QuickReplies struct {
	Rows [][]string `json:"rows"`
	At   time.Time  `json:"at"`
}

const (
	quickRepliesTTL      = 24 * time.Hour
	maxQuickReplyRows    = 6
	maxQuickReplyPerRow  = 4
	maxQuickReplyRunes   = 40
	maxQuickReplyTotal   = 16
	quickRepliesFileName = "quick_replies.json"
)

// quickRepliesFile is its own state file for the same reason as the
// objective's: chat_state.json decodes strictly.
type quickRepliesFile struct {
	ThreadID string     `json:"thread_id"`
	Rows     [][]string `json:"rows"`
	At       time.Time  `json:"at"`
}

func (s *Store) quickRepliesPath() string {
	return filepath.Join(s.statePath(), quickRepliesFileName)
}

// cleanQuickReplies trims, drops empty and repeated chips and enforces the
// limits a phone can show.
func cleanQuickReplies(rows [][]string) ([][]string, error) {
	var out [][]string
	seen := map[string]bool{}
	total := 0
	for _, row := range rows {
		var kept []string
		for _, chip := range row {
			chip = strings.Join(strings.Fields(chip), " ")
			if chip == "" || seen[chip] {
				continue
			}
			if len([]rune(chip)) > maxQuickReplyRunes {
				return nil, fmt.Errorf("a quick reply is longer than %d characters: %q", maxQuickReplyRunes, chip)
			}
			seen[chip] = true
			kept = append(kept, chip)
		}
		if len(kept) == 0 {
			continue
		}
		if len(kept) > maxQuickReplyPerRow {
			return nil, fmt.Errorf("a row holds at most %d quick replies", maxQuickReplyPerRow)
		}
		total += len(kept)
		out = append(out, kept)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one quick reply is required")
	}
	if len(out) > maxQuickReplyRows || total > maxQuickReplyTotal {
		return nil, fmt.Errorf("at most %d quick replies in %d rows", maxQuickReplyTotal, maxQuickReplyRows)
	}
	return out, nil
}

// SetQuickReplies replaces the chips under Brain's latest message.
func (s *Store) SetQuickReplies(rows [][]string) (QuickReplies, error) {
	rows, err := cleanQuickReplies(rows)
	if err != nil {
		return QuickReplies{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readChatStateLocked("")
	if err != nil {
		return QuickReplies{}, err
	}
	threadID := strings.TrimSpace(state.ThreadID)
	if threadID == "" {
		return QuickReplies{}, fmt.Errorf("Brain has no current chat thread")
	}
	file := quickRepliesFile{ThreadID: threadID, Rows: rows, At: s.nowUTC()}
	if err := writeJSONFile(s.quickRepliesPath(), file); err != nil {
		return QuickReplies{}, err
	}
	s.broadcastWorkChange("")
	return QuickReplies{Rows: rows, At: file.At}, nil
}

// ClearQuickReplies removes the chips.
func (s *Store) ClearQuickReplies() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.quickRepliesPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.broadcastWorkChange("")
	return nil
}

// DisplayedQuickReplies returns the chips the apps should show now: those of
// the current thread that have not expired. The app also hides them once the
// user sends a message after QuickReplies.At.
func (s *Store) DisplayedQuickReplies() (*QuickReplies, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.quickRepliesPath())
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(raw)) == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file quickRepliesFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode Brain quick replies: %w", err)
	}
	state, err := s.readChatStateLocked("")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(file.ThreadID) == "" || file.ThreadID != strings.TrimSpace(state.ThreadID) ||
		len(file.Rows) == 0 || s.nowUTC().Sub(file.At) >= quickRepliesTTL {
		return nil, nil
	}
	return &QuickReplies{Rows: file.Rows, At: file.At}, nil
}
