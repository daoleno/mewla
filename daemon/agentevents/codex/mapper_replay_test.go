package codex

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// On thread/resume Codex sends the last usage report again, naming the turn
// it counted, which is over. A daemon that restarted and attached took it for
// a running turn: its Session read as working (and held model picks) until
// its next turn ended.
func TestAUsageReportOfAFinishedTurnOnAttachStartsNoTurn(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	if got := m.Notification("thread/tokenUsage/updated", raw(`{"threadId":"`+thread+`","turnId":"`+turn1+`","tokenUsage":{}}`), t0); got != nil {
		t.Fatalf("the attach's usage report of a finished turn = %+v, want nothing", got)
	}
	// While the thread is known to run a turn whose id is not seen yet, a
	// usage report names it.
	m.Notification("thread/status/changed", raw(`{"threadId":"`+thread+`","status":{"type":"active"}}`), t0)
	got := m.Notification("thread/tokenUsage/updated", raw(`{"threadId":"`+thread+`","turnId":"t2","tokenUsage":{}}`), t0)
	if k := kinds(got); len(k) != 2 || k[0] != agentevents.KindTurnStarted || got[0].TurnID != "t2" {
		t.Fatalf("usage report of the running turn = %v", k)
	}
	if got := m.Notification("thread/tokenUsage/updated", raw(`{"threadId":"`+thread+`","turnId":"t2","tokenUsage":{}}`), t0); len(got) != 1 || got[0].Kind != agentevents.KindProgress {
		t.Fatalf("usage report during the turn = %+v, want progress", got)
	}
}

// A notification of a turn that comes after its end never opens it again.
func TestATurnThatEndedIsNotStartedAgainByItsLateNotifications(t *testing.T) {
	m := NewMapper("%4")
	started(m)
	at := t0.Add(time.Second)
	m.Notification("turn/started", raw(`{"threadId":"`+thread+`","turn":{"id":"t2"}}`), at)
	m.Notification("turn/completed", raw(`{"threadId":"`+thread+`","turn":{"id":"t2","status":"completed","error":null}}`), at)
	for _, method := range []string{"thread/tokenUsage/updated", "item/completed", "turn/diff/updated"} {
		got := m.Notification(method, raw(`{"threadId":"`+thread+`","turnId":"t2","item":{"type":"agentMessage","id":"a"}}`), at)
		for _, e := range got {
			if e.Kind == agentevents.KindTurnStarted {
				t.Fatalf("%s after the turn ended started it again: %+v", method, got)
			}
		}
	}
	if m.Turn() != "" {
		t.Fatalf("running turn = %q after it ended", m.Turn())
	}
	// The next turn starts as usual.
	if got := m.Notification("turn/started", raw(`{"threadId":"`+thread+`","turn":{"id":"t3"}}`), at); len(got) != 1 || got[0].Kind != agentevents.KindTurnStarted {
		t.Fatalf("next turn = %+v", got)
	}
}
