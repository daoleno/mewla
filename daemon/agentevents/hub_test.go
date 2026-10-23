package agentevents

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestHubTracksATurnFromStartToFinish(t *testing.T) {
	h := NewHub()
	h.Publish(Event{WorkerID: "w", Kind: KindBound, Binding: &Binding{ConversationID: "c1", By: "launch"}})
	h.Publish(Event{WorkerID: "w", Kind: KindTurnStarted, TurnID: "t1", At: t0})
	h.Publish(Event{WorkerID: "w", Kind: KindProgress, At: t0.Add(time.Second)})
	s, ok := h.State("w")
	if !ok || !s.Bound || s.Binding.ConversationID != "c1" || s.Current == nil || s.Current.Status != TurnRunning ||
		!s.Current.ProgressAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("running state = %+v", s)
	}
	h.Publish(Event{WorkerID: "w", Kind: KindTurnFinished, TurnID: "t1", Status: TurnCompleted, At: t0.Add(2 * time.Second)})
	s, _ = h.State("w")
	if s.Current.Status != TurnCompleted || !s.Current.StartedAt.Equal(t0) || !s.Current.SettledAt.Equal(t0.Add(2*time.Second)) ||
		len(s.Terminals) != 1 || s.Terminals[0].ID != "t1" {
		t.Fatalf("finished state = %+v / %+v", s.Current, s.Terminals)
	}
}

func TestHubTreatsARetriedFailureAsProgress(t *testing.T) {
	h := NewHub()
	h.Publish(Event{WorkerID: "w", Kind: KindTurnStarted, TurnID: "t1", At: t0})
	h.Publish(Event{WorkerID: "w", Kind: KindTurnFinished, TurnID: "t1", Status: TurnFailed, At: t0.Add(time.Second),
		Error: &TurnError{Class: ErrorNetwork, HTTPStatus: 404, WillRetry: true}})
	s, _ := h.State("w")
	if s.Current.Status != TurnRunning || s.Current.Error == nil || len(s.Terminals) != 0 {
		t.Fatalf("a retried failure ended the turn: %+v", s.Current)
	}
	h.Publish(Event{WorkerID: "w", Kind: KindTurnFinished, TurnID: "t1", Status: TurnFailed, At: t0.Add(2 * time.Second),
		Error: &TurnError{Class: ErrorNetwork, HTTPStatus: 404}})
	s, _ = h.State("w")
	if s.Current.Status != TurnFailed || s.Current.Error.HTTPStatus != 404 || len(s.Terminals) != 1 {
		t.Fatalf("final failure = %+v", s.Current)
	}
}

func TestHubKeepsPendingApprovalsUntilResolved(t *testing.T) {
	h := NewHub()
	h.Publish(Event{WorkerID: "w", Kind: KindApprovalRequest, Approval: &Approval{ID: "a1", Options: []ApprovalOption{{ID: "accept", Scope: "once"}}}})
	h.Publish(Event{WorkerID: "w", Kind: KindApprovalRequest, Approval: &Approval{ID: "a2"}})
	if s, _ := h.State("w"); len(s.Approvals) != 2 {
		t.Fatalf("approvals = %+v", s.Approvals)
	}
	h.Publish(Event{WorkerID: "w", Kind: KindApprovalResolved, ApprovalID: "a1", By: "agent-ui"})
	if s, _ := h.State("w"); len(s.Approvals) != 1 || s.Approvals[0].ID != "a2" {
		t.Fatalf("approvals = %+v", s.Approvals)
	}
}

func TestHubAdmissionCursorIncreases(t *testing.T) {
	h := NewHub()
	h.Publish(Event{WorkerID: "w", Kind: KindUserMessage, ID: "m1", InputSHA256: "aa", At: t0})
	h.Publish(Event{WorkerID: "w", Kind: KindUserMessage, ID: "m2", InputSHA256: "bb", At: t0.Add(time.Second)})
	s, _ := h.State("w")
	if s.Admission == nil || s.Admission.ID != "m2" || s.Admission.Cursor != 2 || s.Admission.InputSHA256 != "bb" {
		t.Fatalf("admission = %+v", s.Admission)
	}
}

func TestHubSubscriberMissesEventsInsteadOfBlocking(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("w")
	defer cancel()
	for i := 0; i < 200; i++ {
		h.Publish(Event{WorkerID: "w", Kind: KindProgress, At: t0.Add(time.Duration(i) * time.Millisecond)})
	}
	if got := len(ch); got != 64 {
		t.Fatalf("buffered %d events, want the bound of 64", got)
	}
	s, _ := h.State("w")
	if !s.LastEvent.Equal(t0.Add(199 * time.Millisecond)) {
		t.Fatalf("state missed events a subscriber dropped: %v", s.LastEvent)
	}
}

func TestHubExitDisconnects(t *testing.T) {
	h := NewHub()
	h.SetConnected("w", true)
	code := 1
	h.Publish(Event{WorkerID: "w", Kind: KindExit, ExitCode: &code})
	s, _ := h.State("w")
	if !s.Exited || s.Connected || s.ExitCode == nil || *s.ExitCode != 1 {
		t.Fatalf("state = %+v", s)
	}
	h.Forget("w")
	if _, ok := h.State("w"); ok {
		t.Fatal("forgotten worker still known")
	}
}
