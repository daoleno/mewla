package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainExecutorSwitchRecoversMismatchedTranscriptOverSocket(t *testing.T) {
	for _, pair := range [][2]string{{"claude", "codex"}, {"codex", "claude"}} {
		t.Run(pair[0]+"_to_"+pair[1], func(t *testing.T) {
			root := t.TempDir()
			store, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			const thread, host = "thread-switch", "new-host"
			if err := store.SetChatState(brain.ChatState{ThreadID: thread}); err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{"codex": filepath.Join(root, "rollout.jsonl"), "claude": filepath.Join(root, "claude.jsonl")}
			write := func(provider, body string) {
				t.Helper()
				if provider == "codex" {
					writeServerCodexRollout(t, paths[provider], "codex-native", "continue", body, time.Now().UTC())
					return
				}
				file, err := os.Create(paths[provider])
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				for _, item := range [][2]string{{"user", "continue"}, {"assistant", body}} {
					if err := json.NewEncoder(file).Encode(map[string]any{"type": item[0], "uuid": item[0] + body, "sessionId": "claude-native", "cwd": root, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": item[0], "content": []map[string]string{{"type": "text", "text": item[1]}}}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			write(pair[0], "prior host reply")
			write(pair[1], "reply after executor switch")
			oldIdentity := work.HostTranscriptIdentity{Provider: pair[0], SessionID: pair[0] + "-native", Path: paths[pair[0]]}
			old, err := work.LoadHostConversationByIdentity(oldIdentity)
			if err != nil {
				t.Fatal(err)
			}
			service := brain.NewService(store, nil, nil)
			if err := service.MaterializeProviderConversation(thread, old); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostSession(host, pair[1]); err != nil {
				t.Fatal(err)
			}
			// Reproduce the observed persisted state: the new executor owns the
			// host, but an old in-flight resolver wrote back its native transcript.
			if err := store.SetHostProviderTranscript(oldIdentity.SessionID, oldIdentity.Path, ""); err != nil {
				t.Fatal(err)
			}
			liveIdentity := work.HostTranscriptIdentity{Provider: pair[1], SessionID: pair[1] + "-native", Path: paths[pair[1]]}
			srv := &Server{brain: service, watcher: watcher.New(time.Second), providerConversationLoader: func(*work.ProviderConversationReader, string) (work.CodexConversation, error) {
				return work.LoadHostConversationByIdentity(liveIdentity)
			}}
			conn := openThinProxyTestSocket(t, srv)
			defer conn.Close()
			if err := conn.WriteJSON(clientMessage{Type: "codex_conversation_subscribe", RequestID: "current-stream", TargetID: host, Command: pair[1], StartedAt: json.RawMessage(`"2026-10-01T00:00:00Z"`), ConversationScopeKey: "brain-thread:" + thread}); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var first struct {
				Type         string                 `json:"type"`
				Conversation work.CodexConversation `json:"conversation"`
			}
			if err := conn.ReadJSON(&first); err != nil {
				t.Fatal(err)
			}
			if first.Type != "codex_conversation_snapshot" || !idsContainAssistant(first.Conversation.Events, "prior host reply") || !idsContainAssistant(first.Conversation.Events, "reply after executor switch") {
				t.Fatalf("new host reply lost behind old binding: type=%s events=%+v", first.Type, first.Conversation.Events)
			}
			write(pair[1], "subsequent reply")
			var next struct {
				Type    string                        `json:"type"`
				Upserts []work.CodexConversationEvent `json:"upserts"`
			}
			if err := conn.ReadJSON(&next); err != nil {
				t.Fatal(err)
			}
			if next.Type != "codex_conversation_delta" || !idsContainAssistant(next.Upserts, "subsequent reply") {
				t.Fatalf("stream stopped after switch: %+v", next)
			}
		})
	}
}
