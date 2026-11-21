package main

import (
	"testing"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func TestProbeStateForAnAgyConversationAgyCannotReplay(t *testing.T) {
	unreadable := work.CodexConversation{Reason: "conversation_unreadable: agy acp speaks protocol version 2; Mewla reads version 1"}
	if got := probeStateForConversation(unreadable, nil); got != watcher.ProbeStateUnreadable {
		t.Fatalf("unreadable conversation = %v, want unreadable (the source exists)", got)
	}
	if got := probeStateForConversation(work.CodexConversation{Reason: "transcript_not_found"}, nil); got != watcher.ProbeStateUnlocatable {
		t.Fatalf("not yet stored = %v, want unlocatable", got)
	}
}
