package telegram

import (
	"testing"
	"time"
)

func TestStorePersistsTopicStateAndMakesDispatchAmbiguousOnRestart(t *testing.T) {
	root := t.TempDir()
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 30, 0, 0, time.UTC)
	if err := opened.mutate(func(state *durableState) error {
		state.Topics = []topicMapping{{
			SessionID: "session-1", ThreadID: "thread-1", WorkID: "work-1", ChatID: 77,
			MessageThreadID: 42, Label: "Session one", State: topicStateActive, CreatedAt: now,
		}}
		state.TopicOps = []topicOpRecord{{
			ID: "topic:create:session-2", Kind: topicOpCreate, SessionID: "session-2",
			State: "dispatching", CreatedAt: now,
		}}
		state.TopicProjection["topic:msg:session-1:e1"] = "digest"
		state.TopicMessages["topic:msg:session-1:e1"] = 9
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.snapshot()
	if len(state.Topics) != 1 || state.Topics[0].SessionID != "session-1" || state.Topics[0].MessageThreadID != 42 || state.Topics[0].State != topicStateActive {
		t.Fatalf("topic mapping lost: %+v", state.Topics)
	}
	if len(state.TopicOps) != 1 || state.TopicOps[0].State != "ambiguous" || state.TopicOps[0].Kind != topicOpCreate {
		t.Fatalf("dispatching topic op did not become ambiguous: %+v", state.TopicOps)
	}
	if state.TopicProjection["topic:msg:session-1:e1"] != "digest" || state.TopicMessages["topic:msg:session-1:e1"] != 9 {
		t.Fatalf("topic checkpoint maps lost: %+v", state.TopicProjection)
	}
}

func TestStorePersistsFormattedRowAndMakesDispatchAmbiguousOnRestart(t *testing.T) {
	root := t.TempDir()
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	entity := MessageEntity{Type: "bold", Offset: 0, Length: 4}
	if err := opened.mutate(func(state *durableState) error {
		state.Outbox = []outboxRecord{{
			ID: "formatted", Kind: "send", Text: "rich", PlainText: "rich", Entities: []MessageEntity{entity},
			Variant: formattedVariant, State: "dispatching", CreatedAt: time.Now(),
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	row := reopened.snapshot().Outbox[0]
	if row.State != "ambiguous" || row.Variant != formattedVariant || row.PlainText != "rich" || len(row.Entities) != 1 || row.Entities[0] != entity {
		t.Fatalf("restarted row=%+v", row)
	}
}
