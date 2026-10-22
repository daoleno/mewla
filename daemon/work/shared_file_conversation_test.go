package work

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A phone that reconnects subscribes again with a new reader. The transcript
// has not changed, so the new reader must reuse the parse instead of reading a
// transcript that can be hundreds of MB from the start.
func TestFileConversationParseIsSharedAcrossReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var parses atomic.Int32
	parse := func(string) (CodexConversation, error) {
		parses.Add(1)
		time.Sleep(20 * time.Millisecond)
		return CodexConversation{Available: true, SessionID: "shared"}, nil
	}

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conversation, err := NewProviderConversationReader().loadFileConversation("claude", path, parse)
			if err != nil || conversation.SessionID != "shared" {
				t.Errorf("load = %+v, %v", conversation, err)
			}
		}()
	}
	wg.Wait()
	if got := parses.Load(); got != 1 {
		t.Fatalf("concurrent readers parsed the unchanged file %d times, want 1", got)
	}

	if _, err := NewProviderConversationReader().loadFileConversation("claude", path, parse); err != nil {
		t.Fatal(err)
	}
	if got := parses.Load(); got != 1 {
		t.Fatalf("a later reader parsed the unchanged file again: %d parses", got)
	}

	// A grown transcript is parsed again, once.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	for range 2 {
		if _, err := NewProviderConversationReader().loadFileConversation("claude", path, parse); err != nil {
			t.Fatal(err)
		}
	}
	if got := parses.Load(); got != 2 {
		t.Fatalf("grown file parses = %d, want 2", got)
	}
}
