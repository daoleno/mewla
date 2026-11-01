package computers

import (
	"fmt"
	"github.com/daoleno/mewla/daemon/auth"
	"testing"
)

func TestReplyRoutingSurvivesRestartAttemptsAndHistoryPruning(t *testing.T) {
	a, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var deliveries []Delivery
	origin := Origin{SessionID: "%17", PaneGeneration: "pane-original", WorkID: "work-original"}
	opts := Options{Auth: a, Thread: func() string { return "brain-thread" }, ResolveOrigin: func(string) (Origin, error) { return origin, nil }, Deliver: func(d Delivery) (string, error) { deliveries = append(deliveries, d); return DispositionAccepted, nil }}
	m, err := NewManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe()
	m.state.Peers = []outboundPeer{{Name: "remote", DaemonID: "remote-id"}}
	sent, err := m.send(Request{Computer: "remote", Text: "job", SenderSession: origin.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Handoffs("brain-thread")) != 0 {
		t.Fatal("Worker send leaked into Brain chat")
	}
	m.state.Outbox = nil
	m.state.Handoffs = nil
	if err := saveState(m.dir, m.state); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe()
	msg := Message{ID: "reply", ReplyTo: sent.Message.ID, Kind: KindQuestion, Text: "Which ref?", Attempt: 1}
	for _, attempt := range []int{1, 1, 2} {
		msg.Attempt = attempt
		if r := m.deliver("out:remote-id", msg); r.Disposition != DispositionAccepted {
			t.Fatal(r)
		}
	}
	m, err = NewManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe()
	msg.Attempt = 3
	m.deliver("out:remote-id", msg)
	if len(deliveries) != 1 || deliveries[0].Origin != origin {
		t.Fatalf("deliveries=%+v", deliveries)
	}
	msg.ID = "forged-peer-reply"
	m.deliver("out:other-peer", msg)
	if deliveries[1].Origin != (Origin{}) {
		t.Fatal("cross-peer routing authority")
	}
}

func TestMessageDeliveryClaimAndDefiniteRetry(t *testing.T) {
	for _, first := range []string{DispositionNotSubmitted, DispositionUncertain, ""} {
		t.Run(fmt.Sprintf("first=%s", first), func(t *testing.T) {
			a, _ := auth.NewManager(t.TempDir())
			calls := 0
			opts := Options{Auth: a, Deliver: func(Delivery) (string, error) {
				calls++
				if calls == 1 {
					return first, nil
				}
				return DispositionAccepted, nil
			}}
			m, _ := NewManager(opts)
			defer m.unsubscribe()
			msg := Message{ID: "one", Text: "body", Attempt: 1}
			m.deliver("out:peer", msg)
			m, _ = NewManager(opts)
			defer m.unsubscribe()
			msg.Attempt = 2
			got := m.deliver("out:peer", msg)
			want := 1
			if first == DispositionNotSubmitted {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d result=%+v", calls, want, got)
			}
		})
	}
}

func TestInterruptedDeliveryDoesNotChangeRecipient(t *testing.T) {
	a, _ := auth.NewManager(t.TempDir())
	calls := 0
	opts := Options{Auth: a, Deliver: func(Delivery) (string, error) { calls++; return DispositionAccepted, nil }}
	m, _ := NewManager(opts)
	defer m.unsubscribe()
	m.state.Deliveries["computer:out:peer:one"] = deliveryRecord{Disposition: DispositionUncertain}
	if err := saveState(m.dir, m.state); err != nil {
		t.Fatal(err)
	}
	m, _ = NewManager(opts)
	defer m.unsubscribe()
	got := m.deliver("out:peer", Message{ID: "one", Text: "body", Attempt: 2})
	if calls != 0 || got.Disposition != DispositionUncertain {
		t.Fatalf("calls=%d result=%+v", calls, got)
	}
}

func TestQuestionAndAnswerRouteToTheirOwnSenders(t *testing.T) {
	makeManager := func(name, peer string) (*Manager, *[]Delivery) {
		a, err := auth.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		received := []Delivery{}
		m, err := NewManager(Options{Auth: a, Thread: func() string { return name + "-brain" }, ResolveOrigin: func(s string) (Origin, error) { return Origin{SessionID: s, WorkID: name + "-work"}, nil }, Deliver: func(d Delivery) (string, error) { received = append(received, d); return DispositionAccepted, nil }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.unsubscribe)
		m.state.Peers = []outboundPeer{{Name: peer, DaemonID: peer}}
		return m, &received
	}
	a, ar := makeManager("a", "b")
	b, br := makeManager("b", "a")
	request, err := a.send(Request{Computer: "b", SenderSession: "a-worker", Text: "job"})
	if err != nil {
		t.Fatal(err)
	}
	b.deliver("out:a", sentMessage(t, a, request.Message.ID))
	question, err := b.send(Request{Computer: "a", SenderSession: "b-worker", ReplyTo: request.Message.ID, Kind: KindQuestion, Text: "which ref?"})
	if err != nil {
		t.Fatal(err)
	}
	a.deliver("out:b", sentMessage(t, b, question.Message.ID))
	answer, err := a.send(Request{Computer: "b", SenderSession: "a-worker", ReplyTo: question.Message.ID, Kind: KindAnswer, Text: "main"})
	if err != nil {
		t.Fatal(err)
	}
	b.deliver("out:a", sentMessage(t, a, answer.Message.ID))
	if len(*ar) != 1 || (*ar)[0].Origin.SessionID != "a-worker" || len(*br) != 2 || (*br)[0].Origin.SessionID != "" || (*br)[1].Origin.SessionID != "b-worker" {
		t.Fatalf("a=%+v b=%+v", *ar, *br)
	}
}

func sentMessage(t *testing.T, m *Manager, id string) Message {
	t.Helper()
	for _, item := range m.state.Outbox {
		if item.Message.ID == id {
			return item.Message
		}
	}
	t.Fatal("missing message")
	return Message{}
}

func TestSentMessageIsRequeuedWithSameIdentityOnRestart(t *testing.T) {
	a, _ := auth.NewManager(t.TempDir())
	m, _ := NewManager(Options{Auth: a})
	defer m.unsubscribe()
	m.state.Peers = []outboundPeer{{Name: "peer", DaemonID: "peer"}}
	sent, err := m.send(Request{Computer: "peer", Text: "job"})
	if err != nil {
		t.Fatal(err)
	}
	m.state.Outbox[0].State = StateSent
	if err := saveState(m.dir, m.state); err != nil {
		t.Fatal(err)
	}
	again, err := NewManager(Options{Auth: a})
	if err != nil {
		t.Fatal(err)
	}
	defer again.unsubscribe()
	item := again.state.Outbox[0]
	if item.State != StateQueued || item.Message.ID != sent.Message.ID || item.Message.Attempt != 1 {
		t.Fatalf("restarted=%+v", item)
	}
}
