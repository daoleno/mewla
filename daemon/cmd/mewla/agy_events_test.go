package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/work"
)

func TestAgyLinkMessagesBecomeHubEvents(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	input := agyEventsOf("w1", work.AntigravityLinkMessage{T: "input", ID: "r1", Turn: "c:1", SHA: "abc"}, at)
	if len(input) != 1 || input[0].Kind != agentevents.KindUserMessage || input[0].ClientID != "r1" || input[0].InputSHA256 != "abc" || input[0].TurnID != "" {
		t.Fatalf("input = %+v", input)
	}
	if none := agyEventsOf("w1", work.AntigravityLinkMessage{T: "input", ID: "r1"}, at); none != nil {
		t.Fatalf("an input without its digest proves nothing: %+v", none)
	}
	start := agyEventsOf("w1", work.AntigravityLinkMessage{T: "turn_start", Turn: "c:2"}, at)
	if len(start) != 1 || start[0].Kind != agentevents.KindTurnStarted || start[0].Status != agentevents.TurnRunning || start[0].TurnID != "c:2" {
		t.Fatalf("turn_start = %+v", start)
	}
	activity := agyEventsOf("w1", work.AntigravityLinkMessage{T: "activity", Turn: "c:2", Tool: "run_command", Summary: " echo hi "}, at)
	if len(activity) != 1 || activity[0].Kind != agentevents.KindActivity || activity[0].Activity != "echo hi" || activity[0].ActivityTool != "run_command" {
		t.Fatalf("activity = %+v", activity)
	}
	for stop, status := range map[string]agentevents.TurnStatus{
		"end_turn": agentevents.TurnCompleted, "max_tokens": agentevents.TurnCompleted,
		"cancelled": agentevents.TurnInterrupted, "refusal": agentevents.TurnFailed, "error": agentevents.TurnFailed,
	} {
		end := agyEventsOf("w1", work.AntigravityLinkMessage{T: "turn_end", Turn: "c:2", Stop: stop, Error: "quota"}, at)
		if len(end) != 1 || end[0].Kind != agentevents.KindTurnFinished || end[0].Status != status {
			t.Fatalf("turn_end %s = %+v", stop, end)
		}
		if status == agentevents.TurnFailed && (end[0].Error == nil || end[0].Error.Code != stop || end[0].Error.Message != "quota") {
			t.Fatalf("a failed turn says why: %+v", end[0].Error)
		}
	}
	if other := agyEventsOf("w1", work.AntigravityLinkMessage{T: "hello"}, at); other != nil {
		t.Fatalf("hello is not a hub event: %+v", other)
	}
}
