package server

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

func TestTerminalOpensOnlyOwnedSessions(t *testing.T) {
	srv := New(nil, watcher.New(time.Second), nil, nil, nil, nil, nil)
	for _, id := range []string{"%5", "mh-0123456789abcdef01234567", "", "anything"} {
		// An id the watcher does not own is refused before any backend is
		// asked about it.
		if _, err := srv.openableTerminal(id); err == nil || !strings.Contains(err.Error(), "not an available Mewla-owned Session") {
			t.Fatalf("openableTerminal(%q) = %v, want refused", id, err)
		}
	}
}
