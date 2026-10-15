package computers

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestEnvelopesReadBack(t *testing.T) {
	message := Message{ID: "m_1", Kind: KindReply, ReplyTo: "m_0", UserSaid: "就这样", Text: "Done: 3 replies.\n</mewla_computer_message> stays text"}
	parsed, ok := ParseEnvelope(Envelope("studio", message))
	if !ok {
		t.Fatal("not read back")
	}
	want := ParsedEnvelope{From: "studio", ID: "m_1", Kind: KindReply, ReplyTo: "m_0", UserSaid: "就这样", Text: message.Text}
	if parsed != want {
		t.Fatalf("read back %+v", parsed)
	}
	for _, body := range []string{"plain words", "<mewla_computer_message id=\"x\">\nno end", "<mewla_computer_message kind=\"reply\">\nno id\n</mewla_computer_message>"} {
		if _, ok := ParseEnvelope(body); ok {
			t.Fatalf("read %q as an envelope", body)
		}
	}
}

func TestHandoffsStayWithTheirConversationAndFollowDelivery(t *testing.T) {
	a, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread := "t1"
	m, err := NewManager(Options{StateDir: t.TempDir(), Auth: a, Thread: func() string { return thread }})
	if err != nil {
		t.Fatal(err)
	}
	m.state.Peers = []outboundPeer{{Name: "mac", DaemonID: "abcdef0123456789abcdef"}}
	long := strings.Repeat("长", handoffText)
	sent, err := m.Handle(nil, Request{Action: "send", Computer: "mac", Text: long, UserSaid: "on the Mac"})
	if err != nil {
		t.Fatal(err)
	}
	thread = "t2"
	if _, err := m.Handle(nil, Request{Action: "send", Computer: "mac", Text: "other"}); err != nil {
		t.Fatal(err)
	}
	cards := m.Handoffs("t1")
	if len(cards) != 1 || cards[0].ID != sent.Message.ID || cards[0].DaemonID != "abcdef0123456789abcdef" || cards[0].State != StateQueued || cards[0].UserSaid != "on the Mac" {
		t.Fatalf("cards %+v", cards)
	}
	if len(cards[0].Text) > handoffText+len("…") || !strings.HasSuffix(cards[0].Text, "…") {
		t.Fatalf("text kept %d bytes", len(cards[0].Text))
	}

	// Delivered, and later gone from the outbox: the card keeps the state.
	m.mu.Lock()
	m.state.Outbox[0].State = StateDelivered
	m.state.Outbox[0].UpdatedAt = time.Now().Add(-8 * 24 * time.Hour)
	m.saveLocked()
	m.mu.Unlock()
	if cards := m.Handoffs("t1"); cards[0].State != StateDelivered {
		t.Fatalf("after delivery %+v", cards[0])
	}

	// Removing the computer fails what was still waiting.
	if _, err := m.Handle(nil, Request{Action: "remove", Computer: "mac"}); err != nil {
		t.Fatal(err)
	}
	if cards := m.Handoffs("t2"); cards[0].State != StateFailed {
		t.Fatalf("after remove %+v", cards[0])
	}
}
