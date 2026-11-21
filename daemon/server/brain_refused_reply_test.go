package server

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/work"
)

// 2026-11-21 e2e: the Host Session's live conversation, which Chat overlays on
// its timeline, still showed "API Error: Request rejected (429) …" under each
// Work result Brain was handed while the provider refused it. That delivery is
// replayed and the outage line covers it; a refused reply to the person stays.
func TestBrainChatOverlayLeavesOutTheProvidersRefusalOfAWorkDelivery(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const threadID, hostID = "brain-thread-refused", "%0"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "claude"); err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: brain.NewService(store, nil, nil)}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	stamp := func(seconds int) string { return at.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339Nano) }
	limited := &work.ProviderActivityError{Code: "rate_limit", HTTPStatus: 429, Message: "API Error: Request rejected (429)"}
	delivery := work.FormatDirectWorkEventInput(work.DirectWorkEventInput{
		EventID: "b380d0c827e206fb", WorkID: "32aced53-0000-4000-8000-000000000001", WorkRevision: 7,
		HandlingID: "handling:1", ProviderTurnID: "turn:1", WorkTitle: "QuickWorker", Kind: "turn_done",
		Source: "worker", Summary: "Quick Worker: result ready", NextAction: "read the result",
		ContextRef: "work:32aced53", PayloadRef: "work:32aced53",
	})
	live := work.CodexConversation{Available: true, SessionID: "host-claude", Events: []work.CodexConversationEvent{
		{ID: "u1", Kind: "user_message", Role: "user", Body: delivery, Timestamp: stamp(0)},
		{ID: "a1", Kind: "assistant_message", Role: "assistant", Body: "API Error: Request rejected (429) · cooling down", Timestamp: stamp(1), ProviderError: limited},
		{ID: "u2", Kind: "user_message", Role: "user", Body: "Are you there?", Timestamp: stamp(60)},
		{ID: "a2", Kind: "assistant_message", Role: "assistant", Body: "API Error: Request rejected (429) · still cooling down", Timestamp: stamp(61), ProviderError: limited},
	}}
	got := srv.brainScopedConversation("brain-thread:"+threadID, projectProviderConversation(live, true), time.Now().UTC())
	var replies []string
	for _, event := range got.Events {
		if event.Kind == "assistant_message" {
			replies = append(replies, event.ID)
		}
	}
	if len(replies) != 1 || replies[0] != "a2" {
		t.Fatalf("Chat replies = %q, want only the refused reply to the person (a2)", replies)
	}
}
