package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	claudeadapter "github.com/daoleno/mewla/daemon/agentevents/claude"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/work"
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
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"Stop","session_id":"s-1","prompt_id":"p-1"}`))
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
		hook("claude", "%99", `{"hook_event_name":"Stop","session_id":"s-1","prompt_id":"p"}`),
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

func TestAgentLevelsFollowTheAdapterOnceItsEventsFlow(t *testing.T) {
	execs := work.NewExecutorConfig(map[string]work.Executor{"claude": {Name: "claude", Command: "claude"}})
	adapters := agentevents.NewRegistry()
	adapters.Register("claude", claudeadapter.Adapter{})
	hub := agentevents.NewHub()
	levels := agentLevelsFor(execs, adapters, hub)
	worker := &classifier.Worker{ID: "%7", ExecutorID: "claude", Command: "claude"}
	if got := levels(worker); got["approve"] != "keystroke" {
		t.Fatalf("before any hook the Worker is followed by the fallback: %v", got)
	}
	hub.Publish(agentevents.Event{WorkerID: "%7", Kind: agentevents.KindTurnStarted, TurnID: "p1", At: time.Now()})
	if got := levels(worker); got["approve"] != "hook" || got["events"] != "hook" {
		t.Fatalf("once hooks flow the Worker is described by the adapter: %v", got)
	}
	other := &classifier.Worker{ID: "%8", ExecutorID: "my-proxy", Command: "my-proxy"}
	if got := levels(other); got["approve"] != "keystroke" {
		t.Fatalf("an unknown executor is never promoted: %v", got)
	}
}

func TestAHookWhoseSessionIsNotTheWorkersIsRejected(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	ctx := context.Background()
	// Another Claude that inherited %4's id, with its own session.
	for _, payload := range []string{
		`{"hook_event_name":"SessionStart","session_id":"intruder","transcript_path":"/other/i.jsonl","source":"startup"}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"intruder","prompt_id":"p-x","prompt":"x"}`,
		`{"hook_event_name":"Stop","session_id":"intruder","prompt_id":"p-x"}`,
		`{"hook_event_name":"SessionEnd","session_id":"intruder"}`,
		`{"hook_event_name":"UserPromptSubmit","prompt_id":"p-y","prompt":"no session at all"}`,
	} {
		if resp := s.Handle(ctx, hook("claude", "%4", payload)); !resp.OK || resp.HookOutput != "" {
			t.Fatalf("resp = %+v", resp)
		}
	}
	if len(hub.Workers()) != 0 {
		t.Fatalf("a foreign session moved the Worker: %v", hub.Workers())
	}
	// Its permission request is not shown, and gets no answer from Mewla.
	s.approvalWait = 50 * time.Millisecond
	resp := s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"PermissionRequest","session_id":"intruder","prompt_id":"p","tool_name":"Bash","tool_input":{"command":"rm -rf x"}}`))
	if !resp.OK || resp.HookOutput != "" || len(hub.Workers()) != 0 {
		t.Fatalf("a foreign permission request was taken: %+v workers=%v", resp, hub.Workers())
	}
}

func TestAWorkerFollowsItsOwnClearButNotAStrangerWearingIt(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	ctx := context.Background()
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"s-1","transcript_path":"/proj/s-1.jsonl","source":"startup"}`))
	// A session id from a different project directory is not this Worker's /clear.
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"far","transcript_path":"/elsewhere/far.jsonl","source":"clear"}`))
	// Only a SessionStart can move the Worker to a new conversation.
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"UserPromptSubmit","session_id":"s-2","prompt_id":"p","prompt":"x"}`))
	if state, _ := hub.State("%4"); state.Binding.ConversationID != "s-1" || (state.Current != nil && state.Current.ID == "p") {
		t.Fatalf("state = %+v", state)
	}
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"s-2","transcript_path":"/proj/s-2.jsonl","source":"clear"}`))
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"UserPromptSubmit","session_id":"s-2","prompt_id":"p-2","prompt":"x"}`))
	state, _ := hub.State("%4")
	if state.Binding.ConversationID != "s-2" || state.Current == nil || state.Current.ID != "p-2" {
		t.Fatalf("the Worker did not follow its own /clear: %+v", state)
	}
}

func TestAnApprovalCannotBeAnsweredOnceTheWorkerLeftItsConversation(t *testing.T) {
	s, hub := newTestAgentEvents(&classifier.Worker{ID: "%4", Command: "claude --session-id s-1"})
	ctx := context.Background()
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"s-1","transcript_path":"/proj/s-1.jsonl","source":"startup"}`))
	done := make(chan control.Response, 1)
	go func() {
		done <- s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"PermissionRequest","session_id":"s-1","prompt_id":"p","tool_name":"Bash","tool_input":{"command":"x"}}`))
	}()
	var approval agentevents.Approval
	for i := 0; ; i++ {
		if state, _ := hub.State("%4"); len(state.Approvals) == 1 {
			approval = state.Approvals[0]
			break
		}
		if i > 2000 {
			t.Fatal("approval never appeared")
		}
		time.Sleep(time.Millisecond)
	}
	if approval.ConversationID != "s-1" {
		t.Fatalf("approval does not name its conversation: %+v", approval)
	}
	// The Worker moves on: the pending request is dropped and its hook is
	// released without an answer.
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"s-2","transcript_path":"/proj/s-2.jsonl","source":"clear"}`))
	if resp := <-done; !resp.OK || resp.HookOutput != "" {
		t.Fatalf("a hook of the old conversation was answered: %+v", resp)
	}
	if resp := s.decide("%4", approval.ID, "allow", answeredByApp, ""); resp.OK {
		t.Fatal("an approval of the old conversation was decided")
	}
	// An approval stamped with another conversation is refused even if pending.
	hub.Publish(agentevents.Event{WorkerID: "%4", Kind: agentevents.KindApprovalRequest, At: time.Now(),
		Approval: &agentevents.Approval{ID: "stale", ConversationID: "s-1", Options: []agentevents.ApprovalOption{{ID: "allow"}}}})
	if resp := s.decide("%4", "stale", "allow", answeredByApp, ""); resp.OK {
		t.Fatal("an approval from another conversation was decided")
	}
}

// The Hub is in the daemon's memory, so a restart forgets the SessionStart that
// bound a running Claude. Its next hook of any kind puts the binding back, so
// the transcript is read directly again; a stranger's hook does not.
func TestClaudeBindingSurvivesADaemonRestartOnTheNextHook(t *testing.T) {
	worker := &classifier.Worker{ID: "%4", Command: "claude --session-id s-1"}
	s, hub := newTestAgentEvents(worker)
	ctx := context.Background()
	s.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionStart","session_id":"s-1","transcript_path":"/c/projects/p/s-1.jsonl"}`))
	if got := hubBoundTranscript(hub)("%4"); got != "/c/projects/p/s-1.jsonl" {
		t.Fatalf("bound = %q", got)
	}

	// Restart: a new Hub knows nothing.
	s2, hub2 := newTestAgentEvents(worker)
	if got := hubBoundTranscript(hub2)("%4"); got != "" {
		t.Fatalf("a fresh Hub claims %q", got)
	}
	s2.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"PostToolUse","session_id":"s-1","transcript_path":"/c/projects/p/s-1.jsonl"}`))
	if got := hubBoundTranscript(hub2)("%4"); got != "/c/projects/p/s-1.jsonl" {
		t.Fatalf("after the next hook bound = %q", got)
	}
	// An unchanged binding is not republished.
	before, _ := hub2.State("%4")
	s2.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"PostToolUse","session_id":"s-1","transcript_path":"/c/projects/p/s-1.jsonl"}`))
	after, _ := hub2.State("%4")
	if !after.LastEvent.After(before.LastEvent) && after.Binding != before.Binding {
		t.Fatalf("binding changed: %+v -> %+v", before.Binding, after.Binding)
	}
	// Another Claude that inherited the Worker's id cannot move the binding.
	s2.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"PostToolUse","session_id":"other","transcript_path":"/elsewhere/other.jsonl"}`))
	if got := hubBoundTranscript(hub2)("%4"); got != "/c/projects/p/s-1.jsonl" {
		t.Fatalf("a stranger moved the binding to %q", got)
	}
	// An exited Worker has no transcript to read.
	s2.Handle(ctx, hook("claude", "%4", `{"hook_event_name":"SessionEnd","session_id":"s-1"}`))
	if got := hubBoundTranscript(hub2)("%4"); got != "" {
		t.Fatalf("exited Worker still bound to %q", got)
	}
}
