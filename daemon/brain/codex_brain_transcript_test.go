package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// A Codex Brain keeps its conversation in Codex's own app-server daemon, so
// its processes hold nothing that names it. Brain binds the thread it wrote
// in the Brain workspace since it started, or the one its command resumes, so
// the App's Brain chat follows it.
func TestCodexBrainBindsItsConversationFromCodexsRecords(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := NewStore(filepath.Join(home, ".mewla", "brain"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := store.WorkspacePath()
	sessions := filepath.Join(home, ".codex", "sessions", "2026", "11", "10")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, ".codex", "state_5.sqlite")
	sql := func(statement string) {
		t.Helper()
		if out, err := exec.Command(sqlite3, db, statement).CombinedOutput(); err != nil {
			t.Fatalf("sqlite3: %v %s", err, out)
		}
	}
	sql(`CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);`)
	started := time.Now().Add(-10 * time.Minute)
	thread := func(id string, written time.Time) string {
		path := filepath.Join(sessions, "rollout-2026-11-10T10-00-00-"+id+".jsonl")
		meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": workspace, "originator": "codex-tui"}})
		if err := os.WriteFile(path, append(meta, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, written, written); err != nil {
			t.Fatal(err)
		}
		sql(fmt.Sprintf(`INSERT INTO threads VALUES ('%s', '%s', 1, 1, '%s', 0, 1000, 1000);`, id, path, workspace))
		return path
	}
	before := thread("01a1c34f-0000-7000-8000-00000000000a", started.Add(-time.Hour))
	current := thread("01a1c34f-0000-7000-8000-00000000000b", started.Add(time.Minute))

	bind := func(command string) work.HostTranscriptIdentity {
		t.Helper()
		if err := store.ReplaceHostSessionBinding("mh-brain", "codex", "", "", ""); err != nil {
			t.Fatal(err)
		}
		fw := &fakeWatcher{sessions: map[string]*classifier.Worker{"mh-brain": {ID: "mh-brain", Name: "Brain", Command: command, Cwd: workspace, StartedAt: started}}}
		service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
		identity, err := service.BindHostProviderTranscript()
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	// A new Codex Brain: the thread written since it started.
	if got := bind("codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen -C " + workspace); got.SessionID != "01a1c34f-0000-7000-8000-00000000000b" || got.Path != current {
		t.Fatalf("new Brain bound %+v, want the thread it wrote", got)
	}
	host, err := store.HostSession()
	if err != nil || host.ProviderSessionID != "01a1c34f-0000-7000-8000-00000000000b" || host.TranscriptPath != current {
		t.Fatalf("recorded host %+v, %v", host, err)
	}
	// A resumed Codex Brain: the conversation its command names, written
	// before it started.
	if got := bind("codex resume 01a1c34f-0000-7000-8000-00000000000a"); got.SessionID != "01a1c34f-0000-7000-8000-00000000000a" || got.Path != before {
		t.Fatalf("resumed Brain bound %+v", got)
	}
	// A Brain that has written nothing since it started is not bound to an
	// earlier Brain's conversation.
	if err := os.Chtimes(current, started.Add(-time.Minute), started.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := bind("codex"); got.Bound() || strings.TrimSpace(got.Path) != "" {
		t.Fatalf("bound to an earlier conversation: %+v", got)
	}
}
