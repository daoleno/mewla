package brain

import (
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Live 2026-10-15 Mac state (Mewla v0.2.17, Codex 0.162.1): the managed
// app-server owns the rollout, so the Brain Host never binds a provider
// Session. Admissions keep the Host id ("%0") while provider rows carry the
// Codex thread id. Every send must still show exactly one user bubble.
const (
	unboundHostID      = "%0"
	unboundCodexThread = "01a13e9a-a040-7b03-bd10-233182b94104"
	unboundBrainThread = "brain_1792043651647397000"
)

type unboundHostSend struct {
	receipt    string
	body       string
	createdAt  time.Time
	acceptedAt time.Time
	echoAt     time.Time
	echoOffset int
	// echoBeforeAccept materializes the provider echo before the admission
	// is accepted and projected (Mac message 3).
	echoBeforeAccept bool
}

func unboundHostSends() []unboundHostSend {
	at := func(value string) time.Time {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			panic(err)
		}
		return parsed
	}
	return []unboundHostSend{
		{receipt: "mv99b4f3_friqfi", body: "hi", createdAt: at("2026-10-15T08:08:14.115488Z"), acceptedAt: at("2026-10-15T08:08:16.550609Z"), echoAt: at("2026-10-15T08:08:18.319Z"), echoOffset: 107167},
		{receipt: "mv9c1k2v_sdh3wp", body: "这台电脑的 brain, worker 统一用 codex", createdAt: at("2026-10-15T09:24:46.841311Z"), acceptedAt: at("2026-10-15T09:24:49.311499Z"), echoAt: at("2026-10-15T09:24:49.329Z"), echoOffset: 116018},
		{receipt: "mv9c3t46_u14m3k", body: "模型用 gpt 6 astra high", createdAt: at("2026-10-15T09:26:31.765887Z"), acceptedAt: at("2026-10-15T09:26:34.209959Z"), echoAt: at("2026-10-15T09:26:34.221Z"), echoOffset: 177417, echoBeforeAccept: true},
		// Mid-turn steering: Codex stamps the input before the daemon records
		// transport acceptance, but after the admission was prepared.
		{receipt: "mv9c4ki2_58mrp2", body: "不是在 routing 里描述一下就行了？", createdAt: at("2026-10-15T09:27:07.516235Z"), acceptedAt: at("2026-10-15T09:27:10.005749Z"), echoAt: at("2026-10-15T09:27:09.99Z"), echoOffset: 228011},
	}
}

func unboundHostEcho(send unboundHostSend) work.CodexConversationEvent {
	return work.CodexConversationEvent{
		ID:              fmt.Sprintf("%s:%d", unboundCodexThread, send.echoOffset),
		Kind:            timelineKindUserMessage,
		Role:            "user",
		Body:            send.body,
		Timestamp:       send.echoAt.Format(time.RFC3339Nano),
		AdmissionSHA256: AdmissionDigest(send.body),
	}
}

func unboundHostReply(send unboundHostSend) work.CodexConversationEvent {
	return work.CodexConversationEvent{
		ID:        fmt.Sprintf("%s:%d", unboundCodexThread, send.echoOffset+1000),
		Kind:      timelineKindAssistantMessage,
		Role:      "assistant",
		Body:      "reply to " + send.body,
		Timestamp: send.echoAt.Add(5 * time.Second).Format(time.RFC3339Nano),
	}
}

func newUnboundHostStore(t *testing.T) (*Store, *Service) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: unboundBrainThread}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(unboundHostID, "codex"); err != nil {
		t.Fatal(err)
	}
	return store, NewService(store, nil, nil)
}

func assertOneBubblePerSend(t *testing.T, store *Store, sends []unboundHostSend) {
	t.Helper()
	items, err := store.ThreadTimeline(unboundBrainThread, 0)
	if err != nil {
		t.Fatal(err)
	}
	var users []TimelineItem
	for _, item := range items {
		if item.Kind == timelineKindUserMessage {
			users = append(users, item)
		}
	}
	if len(users) != len(sends) {
		t.Fatalf("want %d user rows, got %d: %+v", len(sends), len(users), users)
	}
	for index, send := range sends {
		row := users[index]
		if row.ID != send.receipt || !row.BrainAdmission || row.AdmissionEchoEventID != unboundHostEcho(send).ID {
			t.Fatalf("user row %d = %+v, want admission %s claiming %s", index, row, send.receipt, unboundHostEcho(send).ID)
		}
	}
}

// The live App subscription materializes the cwd-matched Codex conversation
// while the Host binding stays empty.
func TestUnboundCodexHostEchoesShowOnce(t *testing.T) {
	store, service := newUnboundHostStore(t)
	sends := unboundHostSends()
	conversation := work.CodexConversation{Available: true, SessionID: unboundCodexThread}
	materialize := func() {
		t.Helper()
		if err := store.MaterializeProviderConversation(unboundBrainThread, conversation); err != nil {
			t.Fatal(err)
		}
	}
	for _, send := range sends {
		store.now = func() time.Time { return send.createdAt }
		prepared, created, err := service.PrepareHostUserInput(unboundHostID, send.receipt, send.body, "brain-thread:"+unboundBrainThread)
		if err != nil || !created || prepared.SessionID != unboundHostID {
			t.Fatalf("prepare %s: %+v created=%v err=%v", send.receipt, prepared, created, err)
		}
		conversation.Events = append(conversation.Events, unboundHostEcho(send), unboundHostReply(send))
		if send.echoBeforeAccept {
			materialize()
		}
		store.now = func() time.Time { return send.acceptedAt }
		if err := service.AdmitHostUserInput(prepared); err != nil {
			t.Fatal(err)
		}
		materialize()
		materialize()
	}
	host, err := store.HostSession()
	if err != nil || host.ProviderSessionID != "" {
		t.Fatalf("fixture must keep the Host unbound: %+v %v", host, err)
	}
	assertOneBubblePerSend(t, store, sends)

	// A reconnect snapshot or retry of the same receipt adds nothing.
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, send := range sends {
		if _, created, err := NewService(reopened, nil, nil).PrepareHostUserInput(unboundHostID, send.receipt, send.body, "brain-thread:"+unboundBrainThread); err != nil || created {
			t.Fatalf("retry %s created=%v err=%v", send.receipt, created, err)
		}
	}
	if err := reopened.MaterializeProviderConversation(unboundBrainThread, conversation); err != nil {
		t.Fatal(err)
	}
	assertOneBubblePerSend(t, reopened, sends)
}

// Existing v0.2.17 duplicates heal at daemon startup from durable rows alone,
// without a live provider snapshot.
func TestStartupHealsUnboundCodexHostDuplicates(t *testing.T) {
	store, _ := newUnboundHostStore(t)
	sends := unboundHostSends()
	for _, send := range sends {
		store.now = func() time.Time { return send.createdAt }
		candidate := BrainInputAdmission{
			RequestID: send.receipt, ThreadID: unboundBrainThread,
			HostSessionID: unboundHostID, SessionID: unboundHostID, DisplayBody: send.body,
		}
		if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
			t.Fatalf("prepare created=%v err=%v", created, err)
		}
		store.now = func() time.Time { return send.acceptedAt }
		accepted, _, _, err := store.AcceptBrainInputAdmission(candidate)
		if err != nil {
			t.Fatal(err)
		}
		// Recreate the stored duplicate pair exactly as v0.2.17 wrote it.
		echoRow, ok := timelineItemFromProviderEvent(unboundBrainThread, unboundCodexThread, unboundHostEcho(send))
		if !ok {
			t.Fatal("invalid echo fixture")
		}
		rows := []TimelineItem{brainInputAdmissionTimelineItem(accepted), echoRow}
		if send.echoBeforeAccept {
			rows[0], rows[1] = rows[1], rows[0]
		}
		for _, row := range rows {
			if _, err := store.AppendTimelineItem(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	items, err := store.ThreadTimeline(unboundBrainThread, 0)
	if err != nil || len(items) != 2*len(sends) {
		t.Fatalf("fixture timeline=%d err=%v", len(items), err)
	}

	service := NewService(store, nil, nil)
	if _, err := service.ReconcileSignalSystemStartup(nil, 100); err != nil {
		t.Fatal(err)
	}
	assertOneBubblePerSend(t, store, sends)
}
