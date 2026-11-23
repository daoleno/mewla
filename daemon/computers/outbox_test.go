package computers

import (
	"strings"
	"testing"
	"time"
)

// saveState gets a copy of the Manager's state; pruning it must not shift
// items inside the live outbox (that left duplicate copies on every save).
func TestSaveLeavesLiveOutboxIntact(t *testing.T) {
	m := newTestManager(t, nil)
	old := time.Now().UTC().Add(-2 * keepFinished)
	m.state.Outbox = []outboxItem{
		{PeerKey: "out:p", State: StateDelivered, UpdatedAt: old, Message: Message{ID: "a", Attempt: 1}},
		{PeerKey: "out:p", State: StateQueued, UpdatedAt: time.Now().UTC(), Message: Message{ID: "b", Attempt: 1}},
		{PeerKey: "out:p", State: StateQueued, UpdatedAt: time.Now().UTC(), Message: Message{ID: "c", Attempt: 1}},
	}
	if err := saveState(m.dir, m.state); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range m.state.Outbox {
		ids = append(ids, item.Message.ID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("live outbox became %v", ids)
	}
	saved, err := loadState(m.dir)
	if err != nil || len(saved.Outbox) != 2 || saved.Outbox[0].Message.ID != "b" || saved.Outbox[1].Message.ID != "c" {
		t.Fatalf("saved=%+v err=%v", saved.Outbox, err)
	}
}

// A state file written before that fix holds stale copies; the furthest copy
// wins, so a delivered message is not requeued from a stale "sent" copy.
func TestLoadCollapsesDuplicateOutboxCopies(t *testing.T) {
	m := newTestManager(t, nil)
	now := time.Now().UTC()
	m.state.Outbox = []outboxItem{
		{PeerKey: "out:p", State: StateSent, UpdatedAt: now.Add(time.Second), Message: Message{ID: "done", Attempt: 1}},
		{PeerKey: "out:p", State: StateDelivered, UpdatedAt: now, Message: Message{ID: "done", Attempt: 1}},
		{PeerKey: "out:p", State: StateSent, UpdatedAt: now, Message: Message{ID: "done", Attempt: 1}},
		{PeerKey: "out:p", State: StateQueued, UpdatedAt: now, Message: Message{ID: "retry", Attempt: 1, CreatedAt: now}},
		{PeerKey: "out:p", State: StateQueued, UpdatedAt: now, Message: Message{ID: "retry", Attempt: 3, CreatedAt: now}},
	}
	if err := saveState(m.dir, m.state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewManager(m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.unsubscribe()
	if len(reloaded.state.Outbox) != 2 {
		t.Fatalf("outbox=%+v", reloaded.state.Outbox)
	}
	for _, item := range reloaded.state.Outbox {
		switch item.Message.ID {
		case "done":
			if item.State != StateDelivered {
				t.Fatalf("done=%+v", item)
			}
		case "retry":
			if item.Message.Attempt != 3 {
				t.Fatalf("retry=%+v", item)
			}
		}
	}
	if due := reloaded.claimDue("out:p"); len(due) != 1 || due[0].ID != "retry" {
		t.Fatalf("due=%+v", due)
	}
}
