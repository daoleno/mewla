package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Host's bound conversation is read on every tick of a Brain chat. While
// its transcript holds still the answer is the same events, not a copy of them
// made again; once it grows the answer has the new line.
func TestHostBoundConversationIsNotRebuiltWhileTheTranscriptHoldsStill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "projects", "-repo-host-bound", "bound-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := func(kind, uuid, text string) string {
		record := map[string]any{"type": kind, "cwd": "/repo/host-bound", "sessionId": "bound-session", "uuid": uuid, "timestamp": time.Now().UTC().Format(time.RFC3339Nano)}
		if kind == "assistant" {
			record["message"] = map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": text}}}
		} else {
			record["message"] = map[string]any{"role": "user", "content": text}
		}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw) + "\n"
	}
	// A private activation turn first: the filter has something to drop, so
	// its answer is a copy that is worth keeping.
	body := line("user", "u0", "Brain Host activation contract: private") + line("assistant", "a0", "private reply") +
		line("user", "u1", "hello") + line("assistant", "a1", "hi")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("host", "claude", "bound-session", path, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)

	first, err := service.HostBoundProviderConversation()
	if err != nil || !first.Available || len(first.Events) == 0 {
		t.Fatalf("first read = %#v (%v)", first, err)
	}
	for _, event := range first.Events {
		if strings.Contains(event.Body, "private") {
			t.Fatalf("a private turn was kept: %#v", event)
		}
	}
	second, err := service.HostBoundProviderConversation()
	if err != nil || len(second.Events) != len(first.Events) || &second.Events[0] != &first.Events[0] {
		t.Fatalf("an unchanged transcript was filtered again (%v)", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line("assistant", "a2", "and more")); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	third, err := service.HostBoundProviderConversation()
	if err != nil || len(third.Events) != len(first.Events)+1 || third.Events[len(third.Events)-1].Body != "and more" {
		t.Fatalf("a grown transcript answered %d events (%v), want %d ending in the new line", len(third.Events), err, len(first.Events)+1)
	}
}
