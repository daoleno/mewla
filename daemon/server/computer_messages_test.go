package server

import (
	"sort"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/work"
)

func TestBrainConversationShowsComputerMessagesAsCards(t *testing.T) {
	a, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := computers.NewManager(computers.Options{StateDir: t.TempDir(), Auth: a, Thread: func() string { return "t1" }})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{computers: manager}

	// An answer from a computer that reached this one: shown as its card,
	// without the instructions its Brain was given.
	body := computers.Envelope("studio", computers.Message{ID: "m_9", Kind: computers.KindReply, ReplyTo: "m_1", Text: "Replied to 2 issues."})
	events := []work.CodexConversationEvent{
		{ID: "computer:in:computer-0123456789abcdef:m_9:1", Kind: "user_message", Role: "user", Body: body, Timestamp: "2026-10-15T10:00:00Z"},
		{ID: "u1", Kind: "user_message", Role: "user", Body: "<mewla_computer_message from=\"x\" id=\"y\">\nI typed this\n</mewla_computer_message>"},
	}
	var added []work.CodexConversationEvent
	s.addComputerMessages("t1", events, func(e work.CodexConversationEvent) { added = append(added, e) })
	answer := events[0]
	if answer.Source != computerMessageSource || answer.Kind != "status" || answer.Body != "Replied to 2 issues." ||
		answer.Computer == nil || answer.Computer.Direction != "from" || answer.Computer.Computer != "studio" ||
		answer.Computer.DaemonID != "0123456789abcdef" || answer.Computer.ReplyTo != "m_1" {
		t.Fatalf("answer card %+v %+v", answer, answer.Computer)
	}
	if events[1].Kind != "user_message" {
		t.Fatal("a user's own words became a card")
	}
	if len(added) != 0 {
		t.Fatalf("cards without a send: %+v", added)
	}

	// What this Brain hands over is a card with its delivery.
	sent := handoffEvent(computers.Handoff{ID: "m_1", Computer: "mac", DaemonID: "abc", Kind: computers.KindRequest, Text: "Reply on GitHub", State: computers.StateDelivered})
	if sent.ID != "computer-handoff:m_1" || sent.Source != computerMessageSource || sent.Body != "Reply on GitHub" ||
		sent.Computer.Direction != "to" || sent.Computer.State != computers.StateDelivered || sent.Computer.DaemonID != "abc" {
		t.Fatalf("handoff card %+v %+v", sent, sent.Computer)
	}
}

// A handover sorts by when it was sent, among rows written in the user's
// own zone.
func TestHandoverCardsSortByTime(t *testing.T) {
	sent := time.Date(2026, 10, 15, 2, 1, 0, 0, time.UTC) // 10:01 in UTC+8
	events := []work.CodexConversationEvent{
		handoffEvent(computers.Handoff{ID: "m_1", Computer: "mac", Text: "x", CreatedAt: sent}),
		{ID: "late", Seq: 2, Kind: "assistant_message", Timestamp: "2026-10-15T10:02:00+08:00"},
		{ID: "early", Seq: 1, Kind: "user_message", Timestamp: "2026-10-15T10:00:00+08:00"},
	}
	sort.SliceStable(events, func(i, j int) bool { return brainConversationEventLess(events[i], events[j]) })
	if events[0].ID != "early" || events[1].ID != "computer-handoff:m_1" || events[2].ID != "late" {
		t.Fatalf("order %s %s %s", events[0].ID, events[1].ID, events[2].ID)
	}
}
