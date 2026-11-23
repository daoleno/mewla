package computers

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
)

func newTestManager(t *testing.T, deliver DeliverFunc) *Manager {
	t.Helper()
	a, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(Options{Auth: a, Deliver: deliver})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.unsubscribe)
	return m
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(previous); log.SetFlags(flags) })
	return &buf
}

// A message this computer cannot take yet is logged with its cause once (and
// again only when the cause changes), and status answers for it here instead
// of "no message".
func TestRefusedIncomingMessageIsLoggedAndVisibleInStatus(t *testing.T) {
	logs := captureLog(t)
	cause := errors.New("Worker has an open prompt; reply waits until it is answered")
	m := newTestManager(t, func(Delivery) (string, error) {
		if cause != nil {
			return DispositionNotSubmitted, cause
		}
		return DispositionAccepted, nil
	})
	msg := Message{ID: "m_reply", Kind: KindReply, ReplyTo: "m_ask", Text: "numbers", Attempt: 1}
	for attempt := 1; attempt <= 3; attempt++ {
		msg.Attempt = attempt
		if r := m.deliver("in:peer", msg); r.Disposition != DispositionNotSubmitted {
			t.Fatalf("attempt %d: %+v", attempt, r)
		}
	}
	if got := strings.Count(logs.String(), "m_reply"); got != 1 {
		t.Fatalf("logged %d times, want once:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), cause.Error()) {
		t.Fatalf("log lacks the cause:\n%s", logs)
	}
	status, err := m.status(Request{ID: "m_reply"})
	if err != nil || status.Message == nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	view := *status.Message
	if view.Direction != DirectionIn || view.Disposition != DispositionNotSubmitted || view.Error != cause.Error() ||
		view.Attempt != 3 || view.Kind != KindReply || view.ReplyTo != "m_ask" {
		t.Fatalf("view=%+v", view)
	}

	cause = errors.New("Worker presence is unknown")
	msg.Attempt = 4
	m.deliver("in:peer", msg)
	if got := strings.Count(logs.String(), "m_reply"); got != 2 {
		t.Fatalf("a changed cause was not logged:\n%s", logs)
	}

	cause = nil
	msg.Attempt = 5
	if r := m.deliver("in:peer", msg); r.Disposition != DispositionAccepted {
		t.Fatalf("accept=%+v", r)
	}
	if !strings.Contains(logs.String(), "taken (accepted) after 4 refused attempts") {
		t.Fatalf("log lacks the final take:\n%s", logs)
	}
	status, err = m.status(Request{ID: "m_reply"})
	if err != nil || status.Message == nil || status.Message.State != StateDelivered || status.Message.Error != "" {
		t.Fatalf("status after take=%+v err=%v", status.Message, err)
	}
	if _, err := m.status(Request{ID: "m_unknown"}); err == nil {
		t.Fatal("unknown message found")
	}
}
