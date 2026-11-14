//go:build linux

package work

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// The live Claude registers the Worker's cwd (the worktree) with the session
// id; the transcript is where the conversation was born. The registry binding,
// the host identity and the tmux move all name that one file.
func TestClaudeRegistryResumedInAnotherFolderBindsItsConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "claude")
	bornIn := filepath.Join(t.TempDir(), "workspace", "mewla")
	worktree := filepath.Join(t.TempDir(), "worktrees", "mewla-progress-client")
	for _, dir := range []string{bornIn, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	const id = "68197d1b-d079-4cd2-9c44-c93a18189b6e"
	path := writeResumedClaudeTranscript(t, configDir, bornIn, id, now)
	pid := startClaudeRegistryProcess(t, configDir, worktree, id)

	worker := classifier.Worker{
		ID: "%11", Name: "claude", Cwd: worktree, ProcessID: pid, StartedAt: now.Add(-time.Minute),
		Command: "claude --resume " + id,
	}
	got, err := NewProviderConversationReader().Load(worker, WorkerProviderClaude, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Path != path || got.SessionID != id {
		t.Fatalf("conversation = available %v reason %q path %q id %q, want %q", got.Available, got.Reason, got.Path, got.SessionID, path)
	}
	if sessionID, livePath, ok := ClaudeLiveSessionTranscript(worker); !ok || sessionID != id || livePath != path {
		t.Fatalf("live session = %q %q %v, want %q", sessionID, livePath, ok, path)
	}
	identity, found, err := ResolveLiveHostTranscriptIdentity(worker, WorkerProviderClaude)
	if err != nil || !found || identity.Path != path || identity.SessionID != id {
		t.Fatalf("host identity = %+v found=%v err=%v, want %q", identity, found, err, path)
	}
	if moved := ClaudeTranscriptPath(pid, worktree, id); moved != path {
		t.Fatalf("transcript path = %q, want %q", moved, path)
	}

	// The same Worker launched without naming its conversation (a hand-typed
	// `claude`, then /resume) is still bound by the registry alone.
	worker.Command = "claude"
	got, err = NewProviderConversationReader().Load(worker, WorkerProviderClaude, now)
	if err != nil || !got.Available || got.Path != path {
		t.Fatalf("registry only: available %v reason %q path %q err %v", got.Available, got.Reason, got.Path, err)
	}
}
