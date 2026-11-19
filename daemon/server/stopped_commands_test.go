package server

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

func TestStoppedCommandsLineSaysWhatEndedAndWhatCouldNot(t *testing.T) {
	for _, tc := range []struct {
		commands []StoppedCommand
		want     string
	}{
		{[]StoppedCommand{{Command: "seq 1 30", Ended: true}}, "Stopped 1 command this turn started: seq 1 30"},
		{[]StoppedCommand{{Command: "a", Ended: true}, {Command: "b", Ended: true}}, "Stopped 2 commands this turn started: a, b"},
		{[]StoppedCommand{{Command: "make serve", Reason: "Codex no longer had it"}}, "1 command could not be stopped: make serve (Codex no longer had it)"},
		{[]StoppedCommand{{Command: "a", Ended: true}, {Command: "b"}}, "Stopped 1 command this turn started: a · 1 command could not be stopped: b"},
		{[]StoppedCommand{{Command: "for i in 1 2\ndo echo $i\ndone", Ended: true}}, "Stopped 1 command this turn started: for i in 1 2 …"},
		{[]StoppedCommand{{Command: strings.Repeat("x", 200), Ended: true}}, "Stopped 1 command this turn started: " + strings.Repeat("x", 79) + "…"},
	} {
		if got := StoppedCommandsLine(tc.commands); got != tc.want {
			t.Fatalf("StoppedCommandsLine(%+v) = %q, want %q", tc.commands, got, tc.want)
		}
	}
}

func TestStoppedCommandsJoinOnlyTheirSessionsThread(t *testing.T) {
	s := &Server{}
	at := time.Date(2026, 11, 19, 4, 0, 11, 0, time.UTC)
	s.RecordStoppedCommands(StoppedCommands{SessionID: "%1", TurnID: "t1", At: at, Commands: []StoppedCommand{{Command: "seq 1 30", Ended: true}}})
	s.RecordStoppedCommands(StoppedCommands{SessionID: "%2", TurnID: "t9", At: at, Commands: []StoppedCommand{{Command: "make", Reason: "x"}}})
	s.RecordStoppedCommands(StoppedCommands{SessionID: "%1", TurnID: "t2", At: at})
	if s.stopped.Version() != 2 {
		t.Fatalf("version = %d, want 2: a Stop that ended nothing is no line", s.stopped.Version())
	}
	conversation := s.withStoppedCommands("%1", work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{{ID: "u1", Kind: "user_message"}}})
	if len(conversation.Events) != 2 {
		t.Fatalf("events = %+v", conversation.Events)
	}
	row := conversation.Events[1]
	if row.Kind != "status" || row.Source != "commands_stopped" || row.Status != "stopped" || row.Title != "Stopped 1 command this turn started: seq 1 30" || row.Timestamp != "2026-11-19T04:00:11Z" {
		t.Fatalf("row = %+v", row)
	}
	if other := s.withStoppedCommands("%2", work.CodexConversation{}); len(other.Events) != 1 || other.Events[0].Status != "failed" {
		t.Fatalf("%%2 rows = %+v", other.Events)
	}
}
