package server

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/work"
)

// The Brain thread a client renders shows one bubble per send even when the
// Host transcript is unbound (Codex managed app-server, 2026-10-15 Mac) and the
// live conversation carries the provider's own Session id. Terminal typing
// into the Brain Session still shows, and live-only provider user events
// never add a second bubble.
func TestBrainScopedConversationShowsOneBubblePerSendOnUnboundHost(t *testing.T) {
	for _, executor := range []struct {
		id        string
		sessionID string
		eventID   func(string) string
	}{
		{id: "codex", sessionID: "01a13e9a-a040-7b03-bd10-233182b94104", eventID: func(suffix string) string { return "01a13e9a-a040-7b03-bd10-233182b94104:" + suffix }},
		{id: "claude", sessionID: "7d9bb1ec-38cf-4d56-8862-b8fafcaceffc", eventID: func(suffix string) string { return "claude-record-" + suffix }},
	} {
		t.Run(executor.id, func(t *testing.T) {
			store, err := brain.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const threadID, hostID = "brain-thread-unbound", "%0"
			if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostSession(hostID, executor.id); err != nil {
				t.Fatal(err)
			}
			srv := &Server{brain: brain.NewService(store, nil, nil)}
			scope := "brain-thread:" + threadID
			base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

			userEvent := func(suffix, body string, at time.Time) work.CodexConversationEvent {
				return work.CodexConversationEvent{
					ID: executor.eventID(suffix), Kind: "user_message", Role: "user", Body: body,
					Timestamp: at.Format(time.RFC3339Nano), AdmissionSHA256: brain.AdmissionDigest(body),
				}
			}
			live := work.CodexConversation{Available: true, SessionID: executor.sessionID}
			render := func() []string {
				t.Helper()
				got := srv.brainScopedConversation(scope, live, time.Now().UTC())
				if !got.Available {
					t.Fatalf("unavailable: %#v", got)
				}
				var users []string
				for _, event := range got.Events {
					if event.Kind == "user_message" {
						users = append(users, event.ID)
					}
				}
				return users
			}

			send := func(receipt, body string, preparedAt time.Time, echo work.CodexConversationEvent, echoBeforeAccept bool) {
				t.Helper()
				// Same admission shape PrepareHostUserInput records for an
				// unbound Host: the Session is the Host id.
				candidate := brain.BrainInputAdmission{
					RequestID: receipt, ThreadID: threadID, HostSessionID: hostID,
					SessionID: hostID, DisplayBody: body, CreatedAt: preparedAt,
				}
				if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
					t.Fatalf("prepare %s created=%v err=%v", receipt, created, err)
				}
				live.Events = append(live.Events, echo)
				if echoBeforeAccept {
					render()
				}
				accepted, _, _, err := store.AcceptBrainInputAdmission(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.ProjectBrainInputAdmission(accepted); err != nil {
					t.Fatal(err)
				}
			}

			send("receipt-hi", "hi", base, userEvent("1", "hi", base.Add(4*time.Second)), false)
			// Mid-turn steering: the provider stamps the input before the
			// daemon records acceptance (here: real now), rendered first.
			steer := base.Add(time.Minute)
			send("receipt-steer", "继续", steer, userEvent("2", "继续", steer.Add(2480*time.Millisecond)), true)
			// Terminal typing into the Brain Session has no admission.
			live.Events = append(live.Events, userEvent("3", "typed in the terminal", steer.Add(time.Minute)))
			// A provider without exact user timestamps (Cursor) is live-only.
			untimestamped := userEvent("4", "hi again", time.Time{})
			untimestamped.Timestamp = ""
			send("receipt-untimestamped", "hi again", steer.Add(2*time.Minute), untimestamped, false)

			for pass := 0; pass < 2; pass++ {
				users := render()
				want := map[string]bool{"receipt-hi": true, "receipt-steer": true, executor.eventID("3"): true, "receipt-untimestamped": true}
				if len(users) != len(want) {
					t.Fatalf("pass %d: user rows %v, want %v", pass, users, want)
				}
				for _, id := range users {
					if !want[id] {
						t.Fatalf("pass %d: user rows %v, want %v", pass, users, want)
					}
				}
			}
			host, err := store.HostSession()
			if err != nil || host.ProviderSessionID != "" {
				t.Fatalf("fixture must keep the Host unbound: %+v %v", host, err)
			}
		})
	}
}

// An input whose delivery could not be proven is settled uncertain and never
// projected. If the provider did receive it, its own record is the only user
// row: one bubble, which the App's causal fallback pairs with its local row.
func TestBrainScopedConversationUncertainInputEchoShowsOnce(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const threadID, hostID = "brain-thread-uncertain", "%0"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	preparedAt := time.Now().UTC().Add(-time.Minute)
	candidate := brain.BrainInputAdmission{
		RequestID: "receipt-uncertain", ThreadID: threadID, HostSessionID: hostID,
		SessionID: hostID, DisplayBody: "maybe delivered", CreatedAt: preparedAt,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if _, changed, err := store.SettleBrainInputAdmission(candidate.RequestID, threadID, brain.BrainInputAdmissionUncertain, false); err != nil || !changed {
		t.Fatalf("settle uncertain changed=%v err=%v", changed, err)
	}
	srv := &Server{brain: brain.NewService(store, nil, nil)}
	live := work.CodexConversation{Available: true, SessionID: "codex-native", Events: []work.CodexConversationEvent{{
		ID: "codex-native:1", Kind: "user_message", Role: "user", Body: "maybe delivered",
		Timestamp:       preparedAt.Add(time.Second).Format(time.RFC3339Nano),
		AdmissionSHA256: brain.AdmissionDigest("maybe delivered"),
	}}}
	for pass := 0; pass < 2; pass++ {
		got := srv.brainScopedConversation("brain-thread:"+threadID, live, time.Now().UTC())
		var users []string
		for _, event := range got.Events {
			if event.Kind == "user_message" {
				users = append(users, event.ID)
			}
		}
		if len(users) != 1 || users[0] != "codex-native:1" {
			t.Fatalf("pass %d: user rows %v", pass, users)
		}
	}
}
