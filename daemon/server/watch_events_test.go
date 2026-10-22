package server

import (
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func TestWatchEventsStayOutOfTheConversation(t *testing.T) {
	events := []work.CodexConversationEvent{
		{ID: "u1", Kind: "user_message", Body: "hi"},
		{ID: "watch:brief:abc", Kind: "user_message", Body: "<mewla_watch kind=\"brief\">"},
		{ID: "a1", Kind: "assistant_message", Body: "1. Terminal scroll"},
	}
	got := withoutWatchEvents(events)
	if len(got) != 2 || got[0].ID != "u1" || got[1].ID != "a1" {
		t.Fatalf("events = %#v", got)
	}
}
