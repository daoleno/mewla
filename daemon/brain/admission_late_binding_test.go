package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Receipt preparation can precede discovery of the Host's native transcript.
func TestAdmissionEchoAfterLateHostTranscriptBinding(t *testing.T) {
	for _, raced := range []bool{false, true} {
		t.Run(map[bool]string{false: "live echo", true: "durable raced echo"}[raced], func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			const thread, hostID, nativeID = "thread-late", "brain-host:@late", "provider-late"
			if err := store.SetChatState(ChatState{ThreadID: thread}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			service := NewService(store, nil, nil)
			base := time.Date(2026, 10, 1, 8, 57, 18, 0, time.UTC)
			store.now = func() time.Time { return base }
			prepared, created, err := service.PrepareHostUserInput(hostID, "receipt-late", "same input", "brain-thread:"+thread)
			if err != nil || !created || prepared.SessionID != hostID {
				t.Fatalf("prepare: %+v created=%v err=%v", prepared, created, err)
			}
			store.now = func() time.Time { return base.Add(2 * time.Second) }
			if err := service.AdmitHostUserInput(prepared); err != nil {
				t.Fatal(err)
			}
			echo := work.CodexConversationEvent{ID: "provider-user", Seq: 37, Kind: "user_message", Role: "user", Body: "same input", Timestamp: base.Add(2143 * time.Millisecond).Format(time.RFC3339Nano)}
			conversation := work.CodexConversation{Available: true, SessionID: nativeID, Events: []work.CodexConversationEvent{echo}}
			if raced {
				item, ok := timelineItemFromProviderEvent(thread, nativeID, echo)
				if !ok {
					t.Fatal("invalid fixture")
				}
				if _, err := store.AppendTimelineItem(item); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.SetHostProviderTranscript(nativeID, "", ""); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 3; pass++ {
				if pass == 1 {
					store, err = NewStore(root)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := store.MaterializeProviderConversation(thread, conversation); err != nil {
					t.Fatal(err)
				}
				rows, err := store.ThreadTimeline(thread, 0)
				if err != nil || len(rows) != 1 || rows[0].ID != prepared.RequestID || rows[0].AdmissionEchoEventID != echo.ID || rows[0].AdmissionEchoSessionID != nativeID {
					t.Fatalf("pass %d: %+v %v", pass, rows, err)
				}
				if !ProviderUserEchoSuppressions(rows, nativeID)[echo.ID] || ProviderUserEchoSuppressions(rows, "other")[echo.ID] {
					t.Fatal("wrong overlay suppression identity")
				}
				persisted, found, err := store.BrainInputAdmission(prepared.RequestID, thread)
				if err != nil || !found || persisted.SessionID != hostID {
					t.Fatalf("immutable admission changed: %+v %v", persisted, err)
				}
			}
			service = NewService(store, nil, nil)
			if _, created, err := service.PrepareHostUserInput(hostID, prepared.RequestID, "same input", "brain-thread:"+thread); err != nil || created {
				t.Fatalf("receipt replay created=%v err=%v", created, err)
			}
			echo.ID = "provider-user-second"
			echo.Timestamp = base.Add(time.Minute).Format(time.RFC3339Nano)
			conversation.Events = append(conversation.Events, echo)
			if err := store.MaterializeProviderConversation(thread, conversation); err != nil {
				t.Fatal(err)
			}
			rows, err := store.ThreadTimeline(thread, 0)
			if err != nil || len(rows) != 2 || rows[1].ID != echo.ID {
				t.Fatalf("distinct identical input collapsed: %+v %v", rows, err)
			}
		})
	}
}

func TestLateBindingCannotClaimAnotherHostsOrBoundSessionsEcho(t *testing.T) {
	for _, originalSession := range []string{"old-host", "old-provider"} {
		t.Run(originalSession, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			hostID := "replacement-host"
			if originalSession == "old-provider" {
				hostID = "old-host" // Native thread changed within the same Host.
			}
			if err := store.SetHostSession(hostID, "codex"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetHostProviderTranscript("replacement-native", "", ""); err != nil {
				t.Fatal(err)
			}
			store.now = func() time.Time { return base }
			candidate := BrainInputAdmission{RequestID: "receipt-old", ThreadID: "thread", HostSessionID: "old-host", SessionID: originalSession, DisplayBody: "same"}
			if _, _, err := store.PrepareBrainInputAdmission(candidate); err != nil {
				t.Fatal(err)
			}
			accepted, _, _, err := store.AcceptBrainInputAdmission(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ProjectBrainInputAdmission(accepted); err != nil {
				t.Fatal(err)
			}
			if err := store.MaterializeProviderConversation("thread", work.CodexConversation{Available: true, SessionID: "replacement-native", Events: []work.CodexConversationEvent{{ID: "unrelated", Kind: "user_message", Body: "same", Timestamp: base.Add(time.Second).Format(time.RFC3339Nano)}}}); err != nil {
				t.Fatal(err)
			}
			rows, err := store.ThreadTimeline("thread", 0)
			if err != nil || len(rows) != 2 || rows[0].AdmissionEchoEventID != "" {
				t.Fatalf("cross-owner echo claimed: %+v %v", rows, err)
			}
		})
	}
}

func TestLateBindingPreservesFailedRetryAndDistinctIdenticalSends(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID, thread, native = "brain-host:@retry", "thread-retry", "native-retry"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: thread}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }
	service := NewService(store, nil, nil)
	failed, created, err := service.PrepareHostUserInput(hostID, "receipt-a", "same", "brain-thread:"+thread)
	if err != nil || !created {
		t.Fatalf("prepare %v %v", created, err)
	}
	if err := service.AbortHostUserInput(failed.RequestID, thread); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript(native, "", ""); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(time.Second) }
	retry, created, err := service.PrepareHostUserInput(hostID, "receipt-a", "same", "brain-thread:"+thread)
	if err != nil || !created || retry.SessionID != failed.SessionID {
		t.Fatalf("retry %+v %v %v", retry, created, err)
	}
	if err := service.AdmitHostUserInput(retry); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(2 * time.Second) }
	second, created, err := service.PrepareHostUserInput(hostID, "receipt-b", "same", "brain-thread:"+thread)
	if err != nil || !created || second.SessionID != native {
		t.Fatalf("second %+v %v %v", second, created, err)
	}
	if err := service.AdmitHostUserInput(second); err != nil {
		t.Fatal(err)
	}
	conversation := work.CodexConversation{Available: true, SessionID: native, Events: []work.CodexConversationEvent{
		{ID: "echo-a", Kind: "user_message", Body: "same", Timestamp: base.Add(1500 * time.Millisecond).Format(time.RFC3339Nano)},
		{ID: "echo-b", Kind: "user_message", Body: "same", Timestamp: base.Add(2500 * time.Millisecond).Format(time.RFC3339Nano)},
	}}
	for pass := 0; pass < 2; pass++ {
		if err := store.MaterializeProviderConversation(thread, conversation); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ThreadTimeline(thread, 0)
		if err != nil || len(rows) != 2 || rows[0].ID != "receipt-a" || rows[1].ID != "receipt-b" || rows[0].AdmissionEchoEventID != "echo-a" || rows[1].AdmissionEchoEventID != "echo-b" {
			t.Fatalf("two sends: %+v %v", rows, err)
		}
	}
}
