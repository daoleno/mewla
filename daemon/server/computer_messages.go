package server

import (
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/work"
)

const computerMessageSource = "computer_message"

// addComputerMessages shows the Brain's messages with the user's other
// computers as cards: each one it handed over, and each answer it got, read
// back from the envelope its Brain was given instead of as raw text.
func (s *Server) addComputerMessages(threadID string, events []work.CodexConversationEvent, appendUnique func(work.CodexConversationEvent)) {
	if s.computers == nil {
		return
	}
	for i, event := range events {
		if event.Kind != "user_message" || !strings.HasPrefix(event.ID, "computer:") {
			continue
		}
		envelope, ok := computers.ParseEnvelope(event.Body)
		if !ok {
			continue
		}
		events[i] = work.CodexConversationEvent{
			ID: event.ID, Seq: event.Seq, Timestamp: event.Timestamp,
			Kind: "status", Role: "assistant", Body: envelope.Text, Source: computerMessageSource,
			Computer: &work.ComputerMessage{
				Direction: "from", Computer: envelope.From, DaemonID: s.computers.DaemonIDForReceipt(event.ID),
				MessageID: envelope.ID, Kind: envelope.Kind, ReplyTo: envelope.ReplyTo, UserSaid: envelope.UserSaid,
			},
		}
	}
	for _, sent := range s.computers.Handoffs(threadID) {
		appendUnique(handoffEvent(sent))
	}
}

func handoffEvent(sent computers.Handoff) work.CodexConversationEvent {
	return work.CodexConversationEvent{
		ID: "computer-handoff:" + sent.ID, Timestamp: sent.CreatedAt.Format(time.RFC3339Nano),
		Kind: "status", Role: "assistant", Body: sent.Text, Source: computerMessageSource,
		Computer: &work.ComputerMessage{
			Direction: "to", Computer: sent.Computer, DaemonID: sent.DaemonID, MessageID: sent.ID,
			Kind: sent.Kind, ReplyTo: sent.ReplyTo, UserSaid: sent.UserSaid, State: sent.State,
		},
	}
}

// withoutWatchEvents hides what Watch hands Brain (triage, urgent and brief
// events). They reach Brain like user input but are not the person speaking;
// what Brain says about them is the conversation.
func withoutWatchEvents(events []work.CodexConversationEvent) []work.CodexConversationEvent {
	kept := events[:0:0]
	for _, event := range events {
		if event.Kind == "user_message" && strings.HasPrefix(event.ID, "watch:") {
			continue
		}
		kept = append(kept, event)
	}
	return kept
}
