package brain

import (
	"testing"
	"time"
)

// A result, or the person's reply, waiting a day with nothing asked is
// Brain's to settle: it is woken for it (owner 2026-11-19, FreerideOpsBatch).
// Anything still moving, asked, or waiting on something is not.
func TestUndecidedIsAResultWaitingADayWithNothingAsked(t *testing.T) {
	now := time.Date(2026, 11, 19, 3, 37, 0, 0, time.UTC)
	dayAgo := now.Add(-undecidedAfter - time.Minute)
	hourAgo := now.Add(-time.Hour)
	back := CurrentWork{ID: "w", Status: WorkWaiting, UnreadResult: true, AttentionSince: &dayAgo}
	cases := []struct {
		name string
		work CurrentWork
		want bool
	}{
		{"a result back a day", back, true},
		{"the person's reply a day ago", CurrentWork{ID: "w", Status: WorkOpen, UserAction: &WorkUserAction{Kind: WorkUserReply}, AttentionSince: &dayAgo}, true},
		{"back an hour", CurrentWork{ID: "w", Status: WorkWaiting, UnreadResult: true, AttentionSince: &hourAgo}, false},
		{"asked the person", func() CurrentWork { w := back; w.Question = "Delete the package?"; return w }(), false},
		{"a Worker on it", func() CurrentWork { w := back; w.AttemptSessionID = "mh-1"; return w }(), false},
		{"waits on something", func() CurrentWork { w := back; w.Wake = &WorkWake{Kind: WorkWakeSessionTerminal, Ref: "x"}; return w }(), false},
		{"Brain reviewing it", func() CurrentWork { w := back; w.AttentionState = WorkAttentionReviewing; return w }(), false},
		{"closed", func() CurrentWork { w := back; w.Status = WorkDone; return w }(), false},
		{"read and quiet", func() CurrentWork { w := back; w.UnreadResult = false; return w }(), false},
	}
	for _, tc := range cases {
		if got := undecided(tc.work, now); got != tc.want {
			t.Errorf("%s: undecided = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNudgeUndecidedLeavesFreshWorkAlone(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	if _, err := store.CreateWork(Work{Title: "fresh", Objective: "just made"}); err != nil {
		t.Fatal(err)
	}
	if got := service.nudgeUndecided(time.Now().Add(48 * time.Hour)); got != 0 {
		t.Fatalf("a Work with no result is not undecided: delivered %d", got)
	}
}

// The person reads a Work's brief without the protocol Mewla appends for the
// Worker; a brief that is only the protocol reads as none.
func TestPersonBriefDropsTheWorkerProtocol(t *testing.T) {
	brief := "Read worklog/x/BRIEF.md and do it.\n\n" + WorkerProtocolHeading + "\nProfile: implementation.\nYou are the delegated Worker."
	if got := PersonBrief(brief); got != "Read worklog/x/BRIEF.md and do it." {
		t.Fatalf("PersonBrief = %q", got)
	}
	if got := PersonBrief(WorkerProtocolHeading + "\nProfile: quick."); got != "" {
		t.Fatalf("protocol only = %q", got)
	}
	if got := PersonBrief("Ship the beta"); got != "Ship the beta" {
		t.Fatalf("plain brief = %q", got)
	}
}
