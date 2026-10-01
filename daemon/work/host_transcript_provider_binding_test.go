package work

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostTranscriptProviderEvidenceRejectsOnlyProvenMismatch(t *testing.T) {
	for _, fixture := range []struct {
		name, body, provider string
		conflict             bool
	}{
		{"claude-as-codex", `{"type":"user","sessionId":"claude-native","message":{"content":"hello"}}`, WorkerProviderCodex, true},
		{"codex-as-claude", `{"type":"session_meta","payload":{"id":"codex-native"}}`, WorkerProviderClaude, true},
		{"claude", `{"type":"assistant","sessionId":"claude-native"}`, WorkerProviderClaude, false},
		{"codex", `{"type":"session_meta","payload":{"id":"codex-native"}}`, WorkerProviderCodex, false},
		{"partial", `{"type":"session_meta","payload":`, WorkerProviderCodex, false},
		{"legacy", `{"type":"event_msg","payload":{"type":"agent_message","message":"legacy"}}`, WorkerProviderCodex, false},
		{"empty", "", WorkerProviderCodex, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "custom-root-transcript.jsonl")
			if err := os.WriteFile(path, []byte(fixture.body+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			identity := HostTranscriptIdentity{Provider: fixture.provider, SessionID: "saved-native", Path: path}
			if got := HostTranscriptIdentityConflictsWithProvider(identity, fixture.provider); got != fixture.conflict {
				t.Fatalf("conflict=%v, want %v", got, fixture.conflict)
			}
			if fixture.conflict {
				conversation, err := NewProviderConversationReader().LoadByIdentity(identity)
				if err != nil || conversation.Available || conversation.Reason != "host_transcript_provider_mismatch" {
					t.Fatalf("wrong provider was allowed to mask live stream: %+v err=%v", conversation, err)
				}
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if HostTranscriptIdentityConflictsWithProvider(identity, fixture.provider) {
				t.Fatal("missing source destroyed resume identity")
			}
		})
	}
}
