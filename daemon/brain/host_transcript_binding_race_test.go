package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func TestHostTranscriptResolutionCannotOverwriteReplacement(t *testing.T) {
	for _, pair := range [][2]string{{"claude", "codex"}, {"codex", "claude"}} {
		t.Run(pair[0]+"_to_"+pair[1], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cwd := store.WorkspacePath()
			claudePath := filepath.Join(home, ".claude", "projects", strings.NewReplacer("/", "-", ".", "-").Replace(cwd), "claude-session.jsonl")
			if err := os.MkdirAll(filepath.Dir(claudePath), 0700); err != nil {
				t.Fatal(err)
			}
			row, _ := json.Marshal(map[string]any{"type": "user", "uuid": "user-one", "sessionId": "claude-session", "cwd": cwd, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": "continue"}})
			if err := os.WriteFile(claudePath, append(row, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			codexPath := filepath.Join(home, "rollout-codex-session.jsonl")
			writeCodexRolloutFixture(t, codexPath, "codex-session", nil)
			paths := map[string]string{"claude": claudePath, "codex": codexPath}
			const oldHost, newHost = "old-host", "new-host"
			if err := store.SetHostSession(oldHost, pair[0]); err != nil {
				t.Fatal(err)
			}
			// Codex discovers the missing native id from its saved rollout. Claude
			// discovers its source from the explicit native session launch token.
			if pair[0] == "codex" {
				if err := store.SetHostProviderTranscript("", codexPath, ""); err != nil {
					t.Fatal(err)
				}
			}
			if pair[0] == "claude" {
				// Between selecting Codex and replacing the old Claude process,
				// a background read must not attach Claude to the new executor.
				if err := store.SetHostExecutorID(pair[1]); err != nil {
					t.Fatal(err)
				}
				transitional := NewService(store, &fakeWatcher{sessions: map[string]*classifier.Worker{
					oldHost: {ID: oldHost, Command: "claude --resume claude-session", Cwd: cwd},
				}}, nil)
				identity, err := transitional.BindHostProviderTranscript()
				if err != nil || identity.Bound() {
					t.Fatalf("departing process rebound selected executor: %+v err=%v", identity, err)
				}
				if err := store.SetHostSession(oldHost, pair[0]); err != nil {
					t.Fatal(err)
				}
			}

			commands := map[string]string{"claude": "claude --resume claude-session", "codex": "codex"}
			fw := &switchingWatcher{fakeWatcher: &fakeWatcher{sessions: map[string]*classifier.Worker{
				oldHost: {ID: oldHost, Command: commands[pair[0]], Cwd: cwd},
				newHost: {ID: newHost, Command: commands[pair[1]], Cwd: cwd},
			}}, switchFn: func() {
				if err := store.SetHostSession(newHost, pair[1]); err != nil {
					t.Fatal(err)
				}
				if err := store.SetHostProviderTranscript(pair[1]+"-session", paths[pair[1]], ""); err != nil {
					t.Fatal(err)
				}
			}}
			service := NewService(store, fw, nil)
			identity, err := service.BindHostProviderTranscript()
			if err != nil {
				t.Fatal(err)
			}
			host, err := store.HostSession()
			if err != nil {
				t.Fatal(err)
			}
			if host.ID != newHost || host.ExecutorID != pair[1] || host.TranscriptPath != paths[pair[1]] || host.ProviderSessionID != pair[1]+"-session" {
				t.Fatalf("old resolver overwrote new host: %+v", host)
			}
			if identity.Provider != pair[1] || identity.Path != paths[pair[1]] {
				t.Fatalf("stale resolution escaped to stream: %+v", identity)
			}
		})
	}
}
