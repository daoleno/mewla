package work

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// makeUnlistable makes a directory whose entries can be opened by name but not
// listed, so a test fails if code lists it instead of naming the file.
func makeUnlistable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("directory listing is not blockable here (running as root?)")
	}
}

func TestClaudeTranscriptIsReadByNameWithoutListingTheProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	cwd := filepath.Join(home, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := claudeProjectDir(filepath.Join(home, ".claude"), cwd)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	path := filepath.Join(projectDir, id+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A second Claude in the same folder, started at the same moment.
	if err := os.WriteFile(filepath.Join(projectDir, "other.jsonl"), []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeUnlistable(t, projectDir)

	worker := classifier.Worker{ID: "%7", Cwd: cwd, Command: "claude --session-id " + id, StartedAt: time.Now()}

	// By the session id Mewla launched it with (nothing reported yet).
	got, found, err := findClaudeTranscript(worker, time.Now())
	if err != nil || !found || got.Path != path {
		t.Fatalf("by launch id: %+v found=%v err=%v", got, found, err)
	}

	// By what its hooks reported, which wins over a stale launch id (/clear).
	other := filepath.Join(projectDir, "other.jsonl")
	ClaudeBoundTranscript = func(id string) string {
		if id == "%7" {
			return other
		}
		return ""
	}
	t.Cleanup(func() { ClaudeBoundTranscript = func(string) string { return "" } })
	got, found, err = findClaudeTranscript(worker, time.Now())
	if err != nil || !found || got.Path != other || got.ID != "other" {
		t.Fatalf("by hub binding: %+v found=%v err=%v", got, found, err)
	}

	// A bound conversation whose file is not written yet has no turn to show,
	// and is not replaced by a guess from the folder.
	ClaudeBoundTranscript = func(string) string { return filepath.Join(projectDir, "not-yet.jsonl") }
	if got, found, err = findClaudeTranscript(worker, time.Now()); err != nil || found {
		t.Fatalf("unwritten transcript: %+v found=%v err=%v", got, found, err)
	}
}

func TestCursorTranscriptIsReadByTheChatIdInItsCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewProviderConversationReader()
	// A markerless project directory named for the folder is how Cursor lays
	// out a project it has not marked.
	if err := os.MkdirAll(filepath.Join(home, cursorProjectDirPrefix, encodeCursorProjectDir(cwd)), 0o755); err != nil {
		t.Fatal(err)
	}
	roots, err := r.cursorProjectRootsForCWD(home, cwd)
	if err != nil || len(roots) == 0 {
		t.Fatalf("no cursor project root for the folder: %v %v", roots, err)
	}
	const id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	transcripts := filepath.Join(roots[0].dir, cursorTranscriptDir)
	chat := filepath.Join(transcripts, id)
	if err := os.MkdirAll(chat, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chat, id+".jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(transcripts, "someone-else"), 0o755); err != nil {
		t.Fatal(err)
	}
	makeUnlistable(t, transcripts)
	worker := classifier.Worker{ID: "%8", Cwd: cwd, Command: "cursor-agent --resume " + id}
	got, found, err := r.findCursorTranscript(worker, time.Now())
	if err != nil || !found || got.Path != path || got.ID != id {
		t.Fatalf("%+v found=%v err=%v", got, found, err)
	}
}
