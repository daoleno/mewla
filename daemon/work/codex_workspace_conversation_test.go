package work

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A Codex Brain's conversation is the latest interactive thread Codex records
// in the Brain workspace; a codex exec run there and threads elsewhere are not.
// No sqlite3 program is on PATH, as on Windows, which ships none: Codex's
// records are read in this process.
func TestCodexWorkspaceConversationIsTheLatestThreadThere(t *testing.T) {
	home := t.TempDir()
	useHome(t, home)
	t.Setenv("PATH", "")
	workspace := filepath.Join(home, ".mewla", "brain", "workspace")
	other := filepath.Join(home, "project")
	sessions := filepath.Join(home, ".codex", "sessions", "2026", "11", "10")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	runSQLite(t, dbPath, `CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);`)
	base := time.Now().Add(-time.Hour)
	thread := func(id, cwd, originator string, archived int, age time.Duration) {
		path := filepath.Join(sessions, "rollout-2026-11-10T10-00-00-"+id+".jsonl")
		writeJSONL(t, path, map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": cwd, "originator": originator}})
		at := base.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		runSQLite(t, dbPath, `INSERT INTO threads VALUES (`+sqlString(id)+`, `+sqlString(path)+`, 1, 1, `+sqlString(cwd)+`, `+string(rune('0'+archived))+`, 1000, 1000);`)
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

// On Windows Codex records a thread's folder in its database in the
// extended-length form (\\?\C:\Users\...\workspace) and the plain one in the
// rollout's session_meta, as a Codex Brain on Windows 11 did. The thread is
// still the Brain's.
func TestCodexWorkspaceTranscriptFindsAWindowsExtendedLengthFolder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Codex records extended-length folders only on Windows")
	}
	home := t.TempDir()
	useHome(t, home)
	workspace := filepath.Join(home, ".mewla", "brain", "workspace")
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "11", "15", "rollout-2026-11-15T00-40-11-01a1de5d-30ec-71a2-b379-4380932a4291.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, rollout,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "01a1de5d-30ec-71a2-b379-4380932a4291", "cwd": workspace, "originator": "codex-tui"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "hi test from phone"}},
	)
	started := time.Now().Add(-time.Minute)
	runSQLite(t, filepath.Join(home, ".codex", "state_5.sqlite"), `CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);
INSERT INTO threads VALUES ('01a1de5d-30ec-71a2-b379-4380932a4291', `+sqlString(rollout)+`, 1, 1, `+sqlString(`\\?\`+workspace)+`, 0, 1000, 1000);`)
	got, ok := CodexWorkspaceTranscript(workspace, started)
	if !ok || got.SessionID != "01a1de5d-30ec-71a2-b379-4380932a4291" || got.Path != rollout {
		t.Fatalf("transcript = %+v, %v", got, ok)
	}
}

func TestCodexRecordedCWDsAddTheExtendedLengthForm(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := codexRecordedCWDs("/home/me/workspace"); len(got) != 1 || got[0] != "/home/me/workspace" {
			t.Fatalf("unix folder spellings = %q", got)
		}
		return
	}
	for cwd, want := range map[string][]string{
		`C:\Users\Docker\.mewla\brain\workspace`: {`C:\Users\Docker\.mewla\brain\workspace`, `\\?\C:\Users\Docker\.mewla\brain\workspace`},
		`\\server\share\repo`:                    {`\\server\share\repo`, `\\?\UNC\server\share\repo`},
		`\\?\C:\repo`:                            {`\\?\C:\repo`},
	} {
		got := codexRecordedCWDs(cwd)
		if len(got) != len(want) || got[0] != want[0] || (len(want) == 2 && got[1] != want[1]) {
			t.Fatalf("codexRecordedCWDs(%q) = %q, want %q", cwd, got, want)
		}
	}
}
