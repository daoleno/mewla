package main

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
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
	refusal := work.AntigravityLinkMessage{T: "turn_end", Turn: "c:3", Stop: "error", Error: "provider refused", ErrorCode: "rate_limit", HTTPStatus: 429}
	end := agyEventsOf("w1", refusal, at)
	if len(end) != 1 || end[0].Error == nil || end[0].Error.Code != "rate_limit" || end[0].Error.HTTPStatus != 429 {
		t.Fatalf("structured refusal in hub event = %+v", end)
	}
	outcome := agyProviderOutcome(refusal, at)
	if outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Code != "rate_limit" || outcome.Error.HTTPStatus != 429 ||
		watcher.ClassifyProviderError(*outcome.Error) != watcher.ProviderErrorTransient {
		t.Fatalf("structured refusal in availability = %+v", outcome)
	}
	if success := agyProviderOutcome(work.AntigravityLinkMessage{T: "turn_end", Turn: "c:4", Stop: "end_turn"}, at.Add(time.Second)); success.Status != "completed" || success.Error != nil {
		t.Fatalf("provider recovery outcome = %+v", success)
	}
	if other := agyEventsOf("w1", work.AntigravityLinkMessage{T: "hello"}, at); other != nil {
		t.Fatalf("hello is not a hub event: %+v", other)
	}
}

func TestAgySocketReportsEveryFastProviderOutcome(t *testing.T) {
	const session = "agy-0b6a3c1e-0000-4000-8000-00000000abcd"
	w := watcher.New(0)
	a := newAgyEvents(context.Background(), agentevents.NewHub(), nil, func() []*classifier.Worker {
		return []*classifier.Worker{{ID: "worker", Command: "mewla agy-session --agy-session " + session}}
	})
	observed := make(chan watcher.ProviderActivityObservation, 3)
	a.providerOutcome = func(outcome watcher.ProviderActivityObservation) {
		w.ObserveProviderOutcome("agy", outcome)
		observed <- outcome
	}
	server, client := net.Pipe()
	defer client.Close()
	go a.serve(server)
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(work.AntigravityLinkMessage{T: "hello", Session: session, Conversation: "conversation"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := encoder.Encode(work.AntigravityLinkMessage{T: "turn_end", Turn: "turn-" + string(rune('0'+i)), Stop: "error", ErrorCode: "rate_limit", HTTPStatus: 429}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-observed:
		case <-time.After(5 * time.Second):
			t.Fatal("agy outcome was not delivered")
		}
		_, down := w.ProviderOutage("antigravity")
		if down != (i == 2) {
			t.Fatalf("outage after %d refusals = %v", i, down)
		}
	}
	if err := encoder.Encode(work.AntigravityLinkMessage{T: "turn_end", Turn: "turn-3", Stop: "end_turn"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("agy recovery was not delivered")
	}
	if _, down := w.ProviderOutage("antigravity"); down {
		t.Fatal("agy recovery left the provider held")
	}
}

// agy links to Mewla before its own setup says which model the Session runs
// on: the menu is offered only once it has.
func TestAgyMenuWaitsForAgyToSayItsModel(t *testing.T) {
	const session = "agy-0b6a3c1e-0000-4000-8000-00000000abce"
	a := newAgyEvents(context.Background(), agentevents.NewHub(), nil, func() []*classifier.Worker {
		return []*classifier.Worker{{ID: "worker", Command: "mewla agy-session --agy-session " + session}}
	})
	server, client := net.Pipe()
	defer client.Close()
	go a.serve(server)
	encoder, decoder := json.NewEncoder(client), json.NewDecoder(client)
	if err := encoder.Encode(work.AntigravityLinkMessage{T: "hello", Session: session, Conversation: "conversation"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.conn("worker") == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	answer := func(current string) {
		var asked work.AntigravityLinkMessage
		if err := decoder.Decode(&asked); err != nil {
			t.Error(err)
			return
		}
		models, _ := json.Marshal(map[string]string{"current": current})
		_ = encoder.Encode(work.AntigravityLinkMessage{T: "reply", Req: asked.Req, Models: models})
	}
	go answer("")
	if a.Linked("worker") {
		t.Fatal("the menu was offered before agy said its model")
	}
	go answer("gemini-4-carbon-low")
	if !a.Linked("worker") {
		t.Fatal("the menu was not offered once agy said its model")
	}
	if !a.Linked("worker") { // no question to agy this time: nothing answers it
		t.Fatal("a Session whose model agy said lost its menu")
	}
}
