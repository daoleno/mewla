package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
)

func newTestAgentEvents(workers ...*classifier.Worker) (*agentEventService, *agentevents.Hub) {
	hub := agentevents.NewHub()
	s := newAgentEventService(hub, func() []*classifier.Worker { return workers })
	s.approvalWait = 2 * time.Second
	return s, hub
}

func hook(source, workerID, payload string) control.AgentEventRequest {
	return control.AgentEventRequest{Source: source, WorkerID: workerID, Payload: []byte(payload)}
}

func TestClaudeHooksPublishTheTurnForTheWorkerNamedByItsEnvironment(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	ctx := context.Background()
	for _, payload := range []string{
		`{"hook_event_name":"SessionStart","session_id":"s-1","transcript_path":"/t.jsonl"}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"s-1","prompt_id":"p-1","prompt":"hello"}`,
	} {
		if resp := s.Handle(ctx, hook("claude", "%4", payload)); !resp.OK {
			t.Fatalf("resp = %+v", resp)
		}
	}
	state, ok := hub.State("%4")
	if !ok || state.Binding.ConversationID != "s-1" || state.Current == nil || state.Current.ID != "p-1" || state.Current.Status != agentevents.TurnRunning {
		t.Fatalf("state = %+v", state)
	}
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"Stop","prompt_id":"p-1"}`))
	if state, _ := hub.State("%4"); state.Current.Status != agentevents.TurnCompleted {
		t.Fatalf("state = %+v", state.Current)
	}
}

func TestClaudeHookFindsItsWorkerBySessionIDWhenTheEnvironmentIsSilent(t *testing.T) {
	s, hub := newTestAgentEvents(
		&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"},
		&classifier.Worker{ID: "%5", Command: "claude --session-id s-2"},
	)
	s.Handle(context.Background(), hook("claude", "", `{"hook_event_name":"UserPromptSubmit","session_id":"s-2","prompt_id":"p-9","prompt":"x"}`))
	if state, ok := hub.State("%5"); !ok || state.Current == nil || state.Current.ID != "p-9" {
		t.Fatalf("%%5 = %+v", state)
	}
	if _, ok := hub.State("%4"); ok {
		t.Fatal("the other Claude Worker was credited")
	}
}

func TestHooksFromAWorkerMewlaDoesNotKnowAreIgnored(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	for _, req := range []control.AgentEventRequest{
		hook("claude", "%99", `{"hook_event_name":"Stop","prompt_id":"p"}`),
		hook("claude", "", `{"hook_event_name":"Stop","session_id":"stranger","prompt_id":"p"}`),
	} {
		if resp := s.Handle(context.Background(), req); !resp.OK || resp.HookOutput != "" {
			t.Fatalf("resp = %+v", resp)
		}
	}
	if len(hub.Workers()) != 0 {
		t.Fatalf("hub learned %v", hub.Workers())
	}
	if resp := s.Handle(context.Background(), hook("someone-else", "%4", `{}`)); resp.OK {
		t.Fatal("an unknown source was accepted")
	}
	if resp := s.Handle(context.Background(), hook("claude", "%4", `not json`)); resp.OK {
		t.Fatal("a malformed payload was accepted")
	}
}

func TestPermissionRequestWaitsForTheUsersDecisionAndAnswersTheHook(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	done := make(chan control.Response, 1)
	go func() {
		done <- s.Handle(context.Background(), hook("claude", "%4",
			`{"hook_event_name":"PermissionRequest","session_id":"s-1","prompt_id":"p-1","tool_name":"Bash","tool_input":{"command":"touch x"}}`))
	}()
	var approval agentevents.Approval
	deadline := time.Now().Add(2 * time.Second)
	for {
		if state, _ := hub.State("%4"); len(state.Approvals) == 1 {
			approval = state.Approvals[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approval never appeared")
		}
		time.Sleep(time.Millisecond)
	}
	if approval.Tool != "Bash" || approval.Summary != "touch x" {
		t.Fatalf("approval = %+v", approval)
	}
	if err := hub.Decide("%4", approval.ID, "allow", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := <-done
	if !resp.OK || !strings.Contains(resp.HookOutput, `"behavior":"allow"`) {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestPermissionRequestLeavesClaudesOwnPromptWhenNobodyDecides(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	s.approvalWait = 30 * time.Millisecond
	resp := s.Handle(context.Background(), hook("claude", "%4",
		`{"hook_event_name":"PermissionRequest","session_id":"s-1","prompt_id":"p-1","tool_name":"Bash","tool_input":{"command":"x"}}`))
	if !resp.OK || resp.HookOutput != "" {
		t.Fatalf("resp = %+v", resp)
	}
	if state, _ := hub.State("%4"); len(state.Approvals) != 0 {
		t.Fatalf("approvals = %+v", state.Approvals)
	}
}

func TestClaudeHookCommandParses(t *testing.T) {
	fields := strings.Fields(claudeHookCommand("/bin/mewla", "/state"))
	var stderr bytes.Buffer
	t.Setenv("MEWLA_STATE_DIR", t.TempDir())
	if err := runAgentEvent(fields[2:], &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("hook command %q is rejected by agent-event: %s", fields, stderr.String())
	}
}

func TestAgentEventsPruneVanishedWorkers(t *testing.T) {
	live := &classifier.Worker{ID: "%1"}
	s, hub := newTestAgentEvents(live)
	now := time.Now()
	s.now = func() time.Time { return now }
	hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindTurnStarted, TurnID: "a", At: now})
	hub.Publish(agentevents.Event{WorkerID: "%9", Kind: agentevents.KindTurnStarted, TurnID: "b", At: now})
	now = now.Add(2 * time.Minute)
	s.Handle(context.Background(), control.AgentEventRequest{Source: "claude", Payload: []byte(`{}`)})
	if _, ok := hub.State("%9"); ok {
		t.Fatal("vanished Worker still held")
	}
	if _, ok := hub.State("%1"); !ok {
		t.Fatal("live Worker dropped")
	}
}
