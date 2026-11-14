package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// A client that asks for first_events gets a thread's first snapshot in parts:
// a snapshot of the newest events, so it paints the end of the thread from a
// few KB, then the older events as the next revisions' deltas (one here: they
// fit one page).
// After both it holds exactly what one snapshot would have carried, and the
// subscription's later deltas build on that.
func TestFirstEventsSendsTheNewestEventsFirst(t *testing.T) {
	f := newBrainTickFixture(t)
	start := time.Now().UTC().Add(-50 * time.Minute)
	for i := 0; i < 20; i++ {
		f.appendLine("user", fmt.Sprintf("question %d", i), start.Add(time.Duration(2*i)*time.Minute))
		f.appendLine("assistant", fmt.Sprintf("answer %d", i), start.Add(time.Duration(2*i+1)*time.Minute))
	}
	whole, wholeEvents := f.reference()
	if len(whole.Events) <= 10 {
		t.Fatalf("the thread has %d events", len(whole.Events))
	}

	request := f.request(f.liveID)
	request.FirstEvents = 10
	f.srv.publishCodexConversationSubscription(
		context.Background(), f.serverConn, request, f.liveID, f.generation, f.reader, &f.live)
	f.takeTicks()

	snapshot := f.nextMessage()
	var head struct {
		Type         string `json:"type"`
		Revision     int64  `json:"revision"`
		Conversation struct {
			Events []json.RawMessage `json:"events"`
		} `json:"conversation"`
	}
	raw, _ := json.Marshal(snapshot)
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	if head.Type != "codex_conversation_snapshot" || len(head.Conversation.Events) != 10 {
		t.Fatalf("first message: %s with %d events, want a snapshot of the newest 10", head.Type, len(head.Conversation.Events))
	}
	newest := whole.Events[len(whole.Events)-10:]
	for i, event := range head.Conversation.Events {
		if id := eventID(t, event); id != newest[i].ID {
			t.Fatalf("snapshot event %d is %s, want %s", i, id, newest[i].ID)
		}
	}
	f.follow(snapshot)

	rest := f.nextMessage()
	var delta struct {
		Type         string            `json:"type"`
		BaseRevision int64             `json:"base_revision"`
		Revision     int64             `json:"revision"`
		Upserts      []json.RawMessage `json:"upserts"`
		Deletes      []string          `json:"deletes"`
	}
	raw, _ = json.Marshal(rest)
	if err := json.Unmarshal(raw, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" || delta.BaseRevision != head.Revision || delta.Revision != head.Revision+1 {
		t.Fatalf("second message: %s %d→%d after snapshot %d", delta.Type, delta.BaseRevision, delta.Revision, head.Revision)
	}
	if len(delta.Upserts) != len(whole.Events)-10 || len(delta.Deletes) != 0 {
		t.Fatalf("the delta carries %d upserts and %d deletes, want the %d older events", len(delta.Upserts), len(delta.Deletes), len(whole.Events)-10)
	}
	f.follow(rest)
	if f.live == nil || f.live.revision != delta.Revision {
		t.Fatalf("the subscription is at revision %v, want %d", f.live, delta.Revision)
	}
	assertSameEvents(t, f.replay, wholeEvents)

	// The next change is an ordinary delta on top of the whole thread.
	f.appendLine("assistant", "one more answer", time.Now().UTC())
	if _, sent := f.tick(); !sent {
		t.Fatal("an appended answer sent nothing")
	}
	after, afterEvents := f.reference()
	if len(after.Events) != len(whole.Events)+1 {
		t.Fatalf("after the append the thread has %d events, want %d", len(after.Events), len(whole.Events)+1)
	}
	assertSameEvents(t, f.replay, afterEvents)
}

// The older events come a page at a time, the newest page first, each the next
// revision: one delta of 200 older events was one call of 125 ms on a dev box
// for an app to take in, and past NSH's 2 s on the owner's phone (11-14).
// After the last page the client holds exactly what one snapshot carries.
func TestFirstEventsSendsTheOlderEventsInPages(t *testing.T) {
	f := newBrainTickFixture(t)
	start := time.Now().UTC().Add(-400 * time.Minute)
	for i := 0; i < 150; i++ {
		f.appendLine("user", fmt.Sprintf("question %d", i), start.Add(time.Duration(2*i)*time.Minute))
		f.appendLine("assistant", fmt.Sprintf("answer %d", i), start.Add(time.Duration(2*i+1)*time.Minute))
	}
	whole, wholeEvents := f.reference()
	older := len(whole.Events) - 10
	if older <= 2*codexConversationOlderPageEvents {
		t.Fatalf("the thread has %d events, want more than two pages before the newest 10", len(whole.Events))
	}

	request := f.request(f.liveID)
	request.FirstEvents = 10
	f.srv.publishCodexConversationSubscription(
		context.Background(), f.serverConn, request, f.liveID, f.generation, f.reader, &f.live)
	f.takeTicks()

	snapshot := f.nextMessage()
	var head struct {
		Revision int64 `json:"revision"`
	}
	raw, _ := json.Marshal(snapshot)
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	f.follow(snapshot)

	revision := head.Revision
	end := len(whole.Events) - 10
	for end > 0 {
		message := f.nextMessage()
		var delta struct {
			Type         string            `json:"type"`
			BaseRevision int64             `json:"base_revision"`
			Revision     int64             `json:"revision"`
			Upserts      []json.RawMessage `json:"upserts"`
		}
		raw, _ := json.Marshal(message)
		if err := json.Unmarshal(raw, &delta); err != nil {
			t.Fatal(err)
		}
		size := min(codexConversationOlderPageEvents, end)
		if delta.Type != "codex_conversation_delta" || delta.BaseRevision != revision || delta.Revision != revision+1 || len(delta.Upserts) != size {
			t.Fatalf("page: %s %d→%d with %d events, want a delta %d→%d with %d", delta.Type, delta.BaseRevision, delta.Revision, len(delta.Upserts), revision, revision+1, size)
		}
		// The newest of the older events first: this page ends where the last began.
		for i, event := range delta.Upserts {
			if id := eventID(t, event); id != whole.Events[end-size+i].ID {
				t.Fatalf("page event %d is %s, want %s", i, id, whole.Events[end-size+i].ID)
			}
		}
		f.follow(message)
		revision = delta.Revision
		end -= size
	}
	if f.live == nil || f.live.revision != revision {
		t.Fatalf("the subscription is at revision %v, want %d", f.live, revision)
	}
	assertSameEvents(t, f.replay, wholeEvents)

	f.appendLine("assistant", "one more answer", time.Now().UTC())
	if _, sent := f.tick(); !sent {
		t.Fatal("an appended answer sent nothing")
	}
	_, afterEvents := f.reference()
	assertSameEvents(t, f.replay, afterEvents)
}

// Without first_events, or for a thread no longer than it asks for, the first
// snapshot is the whole thread in one message, as before.
func TestFirstEventsLeavesShortThreadsWhole(t *testing.T) {
	conversation := work.CodexConversation{Available: true, Events: make([]work.CodexConversationEvent, 5)}
	for _, first := range []int{0, -1, 5, 6, maxConversationFirstEvents + 1} {
		page, older := codexConversationFirstPage(conversation, first)
		if len(page.Events) != 5 || older != nil {
			t.Fatalf("first_events %d split a 5-event thread into %d + %d", first, len(page.Events), len(older))
		}
	}
	page, older := codexConversationFirstPage(conversation, 2)
	if len(page.Events) != 2 || len(older) != 3 || !page.Available {
		t.Fatalf("first_events 2: %d + %d", len(page.Events), len(older))
	}
}

func assertSameEvents(t *testing.T, got, want map[string]json.RawMessage) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the client holds %d events, one snapshot carries %d", len(got), len(want))
	}
	for id, event := range want {
		if string(got[id]) != string(event) {
			t.Fatalf("event %s differs from the one snapshot's", id)
		}
	}
}
