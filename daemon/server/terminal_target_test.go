package server

import (
	"encoding/json"
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

func TestTerminalHistoryRequestCarriesAnOptionalCursor(t *testing.T) {
	var with, without clientMessage
	if err := json.Unmarshal([]byte(`{"type":"terminal_history","session_id":"s","since":{"epoch":5,"next":9}}`), &with); err != nil {
		t.Fatal(err)
	}
	if with.Since == nil || with.Since.Epoch != 5 || with.Since.Next != 9 {
		t.Fatalf("since = %+v, want epoch 5 next 9", with.Since)
	}
	if err := json.Unmarshal([]byte(`{"type":"terminal_history","session_id":"s"}`), &without); err != nil {
		t.Fatal(err)
	}
	if without.Since != nil {
		t.Fatalf("a request without a cursor got %+v: it must keep the snapshot answer", without.Since)
	}
}
