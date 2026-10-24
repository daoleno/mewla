package watcher

import (
	"errors"
	"fmt"
)

// ErrCompletedSessionGenerationChanged means the pane a completed delegated
// Session was recorded in is now a different process or pane generation (tmux
// reuses pane ids). It is terminal for that record: retrying cannot make the
// pane the recorded Session again, and it must not be killed.
var ErrCompletedSessionGenerationChanged = errors.New("generation changed")

func completedGenerationChanged(sessionID string) error {
	return fmt.Errorf("completed Session %s %w", sessionID, ErrCompletedSessionGenerationChanged)
}
