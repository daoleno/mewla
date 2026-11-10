package work

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A Codex Brain's conversation is the latest interactive thread Codex records
// in the Brain workspace; a codex exec run there and threads elsewhere are not.
func TestCodexWorkspaceConversationIsTheLatestThreadThere(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, ".mewla", "brain", "workspace")
	other := filepath.Join(home, "project")
	sessions := filepath.Join(home, ".codex", "sessions", "2026", "11", "10")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	runSQLite(t, sqlite3, dbPath, `CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);`)
	base := time.Now().Add(-time.Hour)
	thread := func(id, cwd, originator string, archived int, age time.Duration) {
		path := filepath.Join(sessions, "rollout-2026-11-10T10-00-00-"+id+".jsonl")
		writeJSONL(t, path, map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": cwd, "originator": originator}})
		at := base.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		runSQLite(t, sqlite3, dbPath, `INSERT INTO threads VALUES (`+sqlString(id)+`, `+sqlString(path)+`, 1, 1, `+sqlString(cwd)+`, `+string(rune('0'+archived))+`, 1000, 1000);`)
	}
	if _, ok := CodexWorkspaceConversation(workspace); ok {
		t.Fatal("a conversation was named before Codex recorded any")
	}
	thread("01a1c34f-0000-7000-8000-000000000001", workspace, "codex_cli_rs", 0, 2*time.Hour)
	thread("01a1c34f-0000-7000-8000-000000000002", workspace, "codex_cli_rs", 0, time.Hour)
	thread("01a1c34f-0000-7000-8000-000000000003", workspace, "codex-exec", 0, 0)
	thread("01a1c34f-0000-7000-8000-000000000004", other, "codex_cli_rs", 0, 0)
	thread("01a1c34f-0000-7000-8000-000000000005", workspace, "codex_cli_rs", 1, 0)
	if got, ok := CodexWorkspaceConversation(workspace); !ok || got != "01a1c34f-0000-7000-8000-000000000002" {
		t.Fatalf("conversation = %q, %v", got, ok)
	}
}
