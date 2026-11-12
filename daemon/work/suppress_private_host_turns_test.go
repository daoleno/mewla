package work

import (
	"fmt"
	"reflect"
	"testing"
)

// suppressPrivateHostTurnsReference is the rule as it was written when every
// call copied the events.
func suppressPrivateHostTurnsReference(events []CodexConversationEvent) []CodexConversationEvent {
	if len(events) == 0 {
		return events
	}
	suppressPrivateOutput := true
	out := make([]CodexConversationEvent, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case "user_message":
			if IsPrivateHostPrompt(event.Body) {
				suppressPrivateOutput = true
				continue
			}
			suppressPrivateOutput = false
			out = append(out, event)
		default:
			if suppressPrivateOutput {
				continue
			}
			out = append(out, event)
		}
	}
	return out
}

func TestSuppressPrivateHostTurnsKeepsWhatItDidNotDrop(t *testing.T) {
	kinds := []struct{ kind, body string }{
		{"user_message", "an ordinary prompt"},
		{"user_message", "Brain Host activation contract: private"},
		{"assistant_message", "a reply"},
		{"tool_call", ""},
	}
	// Every sequence of up to six events from the four shapes.
	var walk func(prefix []CodexConversationEvent)
	walk = func(prefix []CodexConversationEvent) {
		got := SuppressPrivateHostTurns(prefix)
		want := suppressPrivateHostTurnsReference(prefix)
		if len(want) == 0 && len(got) == 0 {
			// Both empty.
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("events %v: got %v, want %v", ids(prefix), ids(got), ids(want))
		}
		if len(want) == len(prefix) && len(prefix) > 0 && &got[0] != &prefix[0] {
			t.Fatalf("events %v dropped nothing and were copied", ids(prefix))
		}
		if len(prefix) == 6 {
			return
		}
		for _, shape := range kinds {
			next := append(append([]CodexConversationEvent(nil), prefix...), CodexConversationEvent{
				ID: fmt.Sprintf("e%d", len(prefix)), Kind: shape.kind, Body: shape.body,
			})
			walk(next)
		}
	}
	walk(nil)
}

func ids(events []CodexConversationEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.ID+":"+event.Kind)
	}
	return out
}
