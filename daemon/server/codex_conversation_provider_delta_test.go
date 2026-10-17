package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/opencodeapi/opencodeapitest"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// P0 regression: non-OpenCode providers (Codex, Claude, Cursor, Grok, Pi) and
// Brain-thread scopes have no authoritative content version, so every poll
// must use the full fingerprint and delta path. A stable-ID in-place update
// (streaming text, tool state, partial flags, Brain work-card changes) must
// produce a delta — the memoized fingerprint shortcut must never suppress it.

func TestNonOpenCodeProvidersFlowStableIDInPlaceDeltas(t *testing.T) {
	baseTime := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	providers := []struct {
		command string
		source  string
	}{
		{command: "codex", source: "codex_rollout"},
		{command: "claude", source: "claude_transcript"},
		{command: "cursor", source: "cursor_transcript"},
		{command: "grok", source: "grok_session"},
		{command: "pi", source: "pi_session"},
	}
	for _, provider := range providers {
		t.Run(provider.command, func(t *testing.T) {
			var loads atomic.Int32
			srv := &Server{
				watcher: watcher.New(time.Second),
				providerConversationLoader: func(_ *work.ProviderConversationReader, workerID string) (work.CodexConversation, error) {
					load := loads.Add(1)
					events := []work.CodexConversationEvent{{
						ID:        "event:streaming",
						Seq:       1,
						Timestamp: baseTime.Format(time.RFC3339Nano),
						Kind:      "assistant_message",
						Role:      "assistant",
						Body:      "First",
						Partial:   load == 1,
					}}
					if load >= 2 {
						// Stable-ID in-place update: same id, changed body and
						// partial state, exactly like streaming text/tool output.
						events[0].Body = "First Second"
						events[0].Partial = false
					}
					return work.CodexConversation{
						Available: true,
						Source:    provider.source,
						Path:      "/provider/session.jsonl",
						SessionID: "provider-session",
						CWD:       "/provider/workspace",
						Events:    events,
					}, nil
				},
			}
			conn := openThinProxyTestSocket(t, srv)
			request := clientMessage{
				Type:      "codex_conversation_subscribe",
				RequestID: "subscription-inplace",
				TargetID:  "provider-agent",
				Cwd:       "/provider/workspace",
				Command:   provider.command,
				StartedAt: json.RawMessage(`"2026-07-16T06:00:00Z"`),
			}
			if err := conn.WriteJSON(request); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				Type         string                 `json:"type"`
				Conversation work.CodexConversation `json:"conversation"`
			}
			if err := conn.ReadJSON(&snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Type != "codex_conversation_snapshot" ||
				snapshot.Conversation.Events[0].Body != "First" ||
				!snapshot.Conversation.Events[0].Partial {
				t.Fatalf("snapshot = %#v", snapshot)
			}
			// The second poll carries the stable-ID in-place update; the full
			// fingerprint path must send a delta with the upserted event.
			var delta struct {
				Type    string                        `json:"type"`
				Upserts []work.CodexConversationEvent `json:"upserts"`
			}
			if err := conn.ReadJSON(&delta); err != nil {
				t.Fatal(err)
			}
			if delta.Type != "codex_conversation_delta" {
				t.Fatalf("second message = %#v", delta)
			}
			if len(delta.Upserts) != 1 ||
				delta.Upserts[0].ID != "event:streaming" ||
				delta.Upserts[0].Body != "First Second" ||
				delta.Upserts[0].Partial {
				t.Fatalf("in-place upsert = %#v", delta.Upserts)
			}
		})
	}
}

func TestBrainThreadScopeFlowsWorkCardDeltas(t *testing.T) {
	baseTime := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Daily papers", "thread-1", "Three papers.", "")
	srv := &Server{
		watcher:  watcher.New(time.Second),
		brain:    service,
		calendar: calendarStore,
		providerConversationLoader: func(_ *work.ProviderConversationReader, workerID string) (work.CodexConversation, error) {
			return work.CodexConversation{
				Available: true,
				Source:    "codex_rollout",
				Path:      "/provider/session.jsonl",
				SessionID: "provider-session",
				CWD:       "/provider/workspace",
				Events: []work.CodexConversationEvent{{
					ID:        "event:stable",
					Seq:       1,
					Timestamp: baseTime.Format(time.RFC3339Nano),
					Kind:      "assistant_message",
					Role:      "assistant",
					Body:      "Stable history.",
				}},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:                 "codex_conversation_subscribe",
		RequestID:            "subscription-brain",
		TargetID:             "provider-agent",
		Cwd:                  "/provider/workspace",
		Command:              "codex",
		StartedAt:            json.RawMessage(`"2026-07-16T06:00:00Z"`),
		ConversationScopeKey: "brain-thread:thread-1",
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	hasWorkCard := func(events []work.CodexConversationEvent) bool {
		for _, event := range events {
			if event.ID == result.ID && event.Source == "calendar_result" {
				return true
			}
		}
		return false
	}
	if !hasWorkCard(snapshot.Conversation.Events) {
		t.Fatalf("first snapshot lacks the Brain work card: %#v", snapshot.Conversation.Events)
	}

	// The Brain overlay changes independently of the provider source: a second
	// result lands while the provider history is byte-identical. The scope is
	// not eligible for the memoized path, so the full fingerprint must emit a
	// delta carrying the new work card.
	second := finishScheduledResult(t, calendarStore, "item-2", "Evening notes", "thread-1", "Second card.", "")
	var delta struct {
		Type    string                        `json:"type"`
		Upserts []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	found := false
	for _, event := range delta.Upserts {
		if event.ID == second.ID && event.Source == "calendar_result" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Brain work-card change missing from delta: %#v", delta.Upserts)
	}
}

func TestBrainThreadScopeFlowsDelegatedWorkCardDelta(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-work-card"}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, nil)
	baseTime := time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)
	srv := &Server{
		watcher: watcher.New(time.Second),
		brain:   service,
		providerConversationLoader: func(_ *work.ProviderConversationReader, _ string) (work.CodexConversation, error) {
			return work.CodexConversation{
				Available: true,
				Source:    "codex_rollout",
				SessionID: "provider-session",
				Events: []work.CodexConversationEvent{{
					ID: "provider-event", Seq: 1, Timestamp: baseTime.Format(time.RFC3339Nano),
					Kind: "assistant_message", Role: "assistant", Body: "Stable history.",
				}},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:                 "codex_conversation_subscribe",
		RequestID:            "subscription-work-card",
		TargetID:             "provider-agent",
		Cwd:                  "/provider/workspace",
		Command:              "codex",
		StartedAt:            json.RawMessage(`"2026-07-16T07:00:00Z"`),
		ConversationScopeKey: "brain-thread:thread-work-card",
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	item, err := store.CreateWork(brain.Work{
		Title: "Delegated smoke", Objective: "verify live card delivery", Status: brain.WorkRunning,
		SourceThreadID: "thread-work-card", CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	const eventID = "delegated-work-card-event"
	if _, created, err := service.AppendWorkEvent(brain.WorkEvent{
		ID: eventID, WorkID: item.ID, Kind: "session.done",
		DedupeKey: "session:worker:turn:one:session.done", PayloadRef: "session:worker",
		SourceName: "worker", Summary: "Delegated provider completed", Actionable: false,
	}); err != nil || !created {
		t.Fatalf("append work event created=%v err=%v", created, err)
	}

	var delta struct {
		Type    string                        `json:"type"`
		Upserts []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	for _, event := range delta.Upserts {
		if event.ID == eventID && event.Source == "work_result" && event.WorkID == item.ID && event.Unread {
			return
		}
	}
	t.Fatalf("delegated Work card missing from delta: %#v", delta.Upserts)
}

func recentOpenCodeServerDeltaFixtureStart(now time.Time) time.Time {
	return now.UTC().Add(-time.Hour).Truncate(time.Second)
}

// openOpenCodeSubscription subscribes to one OpenCode Worker's conversation
// and returns the socket after its snapshot.
func openOpenCodeSubscription(t *testing.T, cwd string, started time.Time) (*websocket.Conn, work.CodexConversation) {
	t.Helper()
	srv := &Server{watcher: watcher.New(time.Second)}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-" + filepath.Base(cwd),
		TargetID:  "agent-" + filepath.Base(cwd),
		Cwd:       cwd,
		Command:   "opencode",
		StartedAt: json.RawMessage(fmt.Sprintf("%q", started.Format(time.RFC3339Nano))),
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	return conn, snapshot.Conversation
}

// A message OpenCode deletes must reach the client as an authoritative delta
// delete.
func TestOpenCodeDeletionFlowsServerDelta(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := recentOpenCodeServerDeltaFixtureStart(time.Now())
	service.PutSession(opencodeapitest.Session{ID: "ses_srvdel", Directory: "/repo/srvdel", Created: started.Add(time.Second), Updated: started.Add(30 * time.Second)})
	service.PutMessage("ses_srvdel", opencodeapitest.User("msg_a", started.Add(time.Second), "alpha"))
	service.PutMessage("ses_srvdel", opencodeapitest.User("msg_b", started.Add(10*time.Second), "beta"))
	conn, snapshot := openOpenCodeSubscription(t, "/repo/srvdel", started)
	ids := map[string]bool{}
	for _, event := range snapshot.Events {
		ids[event.ID] = true
	}
	if !ids["msg_a"] || !ids["msg_b"] {
		t.Fatalf("snapshot events = %v", ids)
	}

	service.RemoveMessage("ses_srvdel", "msg_a")
	var delta struct {
		Type    string   `json:"type"`
		Deletes []string `json:"deletes"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" || !slices.Contains(delta.Deletes, "msg_a") {
		t.Fatalf("delta must delete msg_a: %#v", delta)
	}
}

// A streaming assistant message growing in place must reach the client as an
// upsert carrying the new body.
func TestOpenCodeInPlaceUpdateFlowsMemoizedDelta(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := recentOpenCodeServerDeltaFixtureStart(time.Now())
	at := func(seconds int) time.Time { return started.Add(time.Duration(seconds) * time.Second) }
	service.PutSession(opencodeapitest.Session{ID: "ses_inc", Directory: "/repo/inc", Created: at(1), Updated: at(30)})
	service.PutMessage("ses_inc", opencodeapitest.User("msg_1", at(1), "hello"))
	service.PutMessage("ses_inc", opencodeapitest.Assistant("msg_2", at(2), time.Time{}, opencodeapitest.Text("first")))
	conn, _ := openOpenCodeSubscription(t, "/repo/inc", started)

	service.PutMessage("ses_inc", opencodeapitest.Assistant("msg_2", at(2), time.Time{}, opencodeapitest.Text("first second")))
	var delta struct {
		Type    string                        `json:"type"`
		Upserts []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	found := slices.ContainsFunc(delta.Upserts, func(event work.CodexConversationEvent) bool {
		return event.ID == "msg_2:text:0" && event.Body == "first second"
	})
	if !found {
		t.Fatalf("in-place update missing from delta: %#v", delta.Upserts)
	}
}
