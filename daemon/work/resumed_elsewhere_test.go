package work

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// writeResumedClaudeTranscript writes conversation id where Claude keeps it:
// under the project folder of the cwd it was born in.
func writeResumedClaudeTranscript(t *testing.T, configDir, bornIn, id string, now time.Time) string {
	t.Helper()
	path := filepath.Join(claudeProjectDir(configDir, bornIn), id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, path,
		map[string]any{"type": "mode", "mode": "normal", "sessionId": id},
		map[string]any{
			"type": "user", "cwd": bornIn, "sessionId": id, "uuid": "u-1",
			"timestamp": now.Add(-time.Hour).Format(time.RFC3339Nano),
			"message":   map[string]any{"role": "user", "content": "the first brief"},
		},
		map[string]any{
			"type": "assistant", "cwd": bornIn, "sessionId": id, "uuid": "a-1", "parentUuid": "u-1",
			"timestamp": now.Add(-time.Hour + time.Second).Format(time.RFC3339Nano),
			"message": map[string]any{"role": "assistant", "content": []map[string]any{
				{"type": "text", "text": "working on it"},
			}},
		},
	)
	forceReaderFixtureModTime(t, path, now)
	return path
}

// A Claude resumed in another folder (Brain's -cwd <worktree> -command "claude
// --resume <id>") keeps writing the conversation under the folder it was born
// in; the worktree has no project folder at all. Its Chat was empty
// (transcript_not_found) and its turns went turn_lost (owner 2026-11-13).
func TestClaudeResumedInAnotherFolderReadsItsConversationWhereClaudeKeepsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	configDir := filepath.Join(home, ".claude")
	bornIn := filepath.Join(home, "workspace", "mewla")
	worktree := filepath.Join(home, ".mewla", "worktrees", "mewla-progress-client")
	for _, dir := range []string{bornIn, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	const id = "68197d1b-d079-4cd2-9c44-c93a18189b6e"
	path := writeResumedClaudeTranscript(t, configDir, bornIn, id, now)
	// Another conversation of the same project is not this Worker's.
	writeResumedClaudeTranscript(t, configDir, bornIn, "00000000-0000-0000-0000-000000000000", now)

	worker := classifier.Worker{
		ID: "%9", Name: "claude", Cwd: worktree, StartedAt: now.Add(-time.Minute),
		Command: "claude --resume " + id + " --model claude-opus-5-5",
	}
	got, err := NewProviderConversationReader().Load(worker, WorkerProviderClaude, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != path || got.SessionID != id {
		t.Fatalf("conversation = available %v reason %q path %q id %q, want %q", got.Available, got.Reason, got.Path, got.SessionID, path)
	}
	if len(got.Events) == 0 {
		t.Fatal("resumed conversation has no history")
	}
}

// A Codex resumed in another folder (`codex resume <id>` is not wrapped in an
// app-server, so nothing binds it) is found by its thread id, not by a folder
// it never ran in.
func TestCodexResumedInAnotherFolderReadsItsThreadByID(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	bornIn := filepath.Join(home, "workspace", "mewla")
	worktree := filepath.Join(home, ".mewla", "worktrees", "mewla-progress-client")
	for _, dir := range []string{bornIn, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const id = "019eee5e-7dec-71b1-bc2b-adcb2bad1c4c"
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "11", "13", "rollout-2026-11-13T09-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, rollout,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": bornIn, "originator": "codex-tui"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "the first brief"}},
	)
	other := filepath.Join(filepath.Dir(rollout), "rollout-2026-11-13T09-30-00-019eee5e-0000-0000-0000-000000000000.jsonl")
	writeJSONL(t, other,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "019eee5e-0000-0000-0000-000000000000", "cwd": bornIn, "originator": "codex-tui"}},
	)
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	runSQLite(t, sqlite3, dbPath, `
CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);
INSERT INTO threads VALUES (`+sqlString(id)+`, `+sqlString(rollout)+`, 1794560000, 1794560100, `+sqlString(bornIn)+`, 0, 1794560000000, 1794560100000);
INSERT INTO threads VALUES ('019eee5e-0000-0000-0000-000000000000', `+sqlString(other)+`, 1794561000, 1794561100, `+sqlString(bornIn)+`, 0, 1794561000000, 1794561100000);
`)

	now := time.Now()
	worker := classifier.Worker{
		ID: "%10", Name: "codex", Cwd: worktree, StartedAt: now.Add(-time.Minute),
		Command: "codex resume " + id + " -m gpt-5.5",
	}
	got, err := NewProviderConversationReader().Load(worker, WorkerProviderCodex, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != rollout {
		t.Fatalf("conversation = available %v reason %q path %q, want %q", got.Available, got.Reason, got.Path, rollout)
	}
}
