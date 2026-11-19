package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A result, or the person's reply, waiting a day with nothing asked is
// Brain's to settle: it is woken for it (owner 2026-11-19, FreerideOpsBatch).
// Anything still moving, asked, or waiting on something is not.
func TestUndecidedIsADecisionNobodyTakes(t *testing.T) {
	now := time.Date(2026, 11, 19, 3, 37, 0, 0, time.UTC)
	dayAgo := now.Add(-undecidedAfter - time.Minute)
	hourAgo := now.Add(-time.Hour)
	back := CurrentWork{ID: "w", Status: WorkWaiting, UnreadResult: true, AttentionSince: &dayAgo}
	twoHoursAgo := now.Add(-parkedAfter - time.Minute)
	parked := CurrentWork{ID: "w", Status: WorkWaiting, Wake: &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:b1"}, AttentionSince: &twoHoursAgo}
	cases := []struct {
		name         string
		work         CurrentWork
		parked, want bool
	}{
		{"a result back a day", back, false, true},
		{"the person's reply a day ago", CurrentWork{ID: "w", Status: WorkOpen, UserAction: &WorkUserAction{Kind: WorkUserReply}, AttentionSince: &dayAgo}, false, true},
		{"back an hour", CurrentWork{ID: "w", Status: WorkWaiting, UnreadResult: true, AttentionSince: &hourAgo}, false, false},
		{"parked on the person two hours, nothing asked", parked, true, true},
		{"parked an hour", func() CurrentWork { w := parked; w.AttentionSince = &hourAgo; return w }(), true, false},
		{"parked with no -wait-for (operator ref)", func() CurrentWork {
			w := parked
			w.Wake = &WorkWake{Kind: WorkWakeUserInput, Ref: "operator:w"}
			return w
		}(), true, true},
		{"a named wait: a Mac", func() CurrentWork {
			w := parked
			w.Wake = &WorkWake{Kind: WorkWakeUserInput, Ref: "the Mac mini comes back online"}
			return w
		}(), false, false},
		{"a named wait: a PR head", func() CurrentWork {
			w := parked
			w.Wake = &WorkWake{Kind: WorkWakeUserInput, Ref: "new heads of freeride-aws#108 to re-check"}
			return w
		}(), false, false},
		{"parked with a live delegated Worker", func() CurrentWork {
			w := parked
			w.WorkerSessions = []WorkWorkerSession{{SessionID: "mh-9", Live: true}}
			return w
		}(), false, false},
		{"parked, its Worker finished", func() CurrentWork {
			w := parked
			w.WorkerSessions = []WorkWorkerSession{{SessionID: "mh-9", Live: false}}
			return w
		}(), true, true},
		{"asked the person", func() CurrentWork { w := back; w.Question = "Delete the package?"; return w }(), false, false},
		{"parked and asked", func() CurrentWork { w := parked; w.Question = "Ship it?"; return w }(), false, false},
		{"a Worker on it", func() CurrentWork { w := back; w.AttemptSessionID = "mh-1"; return w }(), false, false},
		{"waits on a Session", func() CurrentWork { w := back; w.Wake = &WorkWake{Kind: WorkWakeSessionTerminal, Ref: "x"}; return w }(), false, false},
		{"Brain reviewing it", func() CurrentWork { w := back; w.AttentionState = WorkAttentionReviewing; return w }(), false, false},
		{"queued for Brain", func() CurrentWork { w := back; w.AttentionState = WorkAttentionQueued; return w }(), false, false},
		{"closed", func() CurrentWork { w := back; w.Status = WorkDone; return w }(), false, false},
		{"read and quiet", func() CurrentWork { w := back; w.UnreadResult = false; return w }(), false, false},
	}
	for _, tc := range cases {
		parked, got := undecided(tc.work, now)
		if got != tc.want || got && parked != tc.parked {
			t.Errorf("%s: undecided = %v (parked %v), want %v (parked %v)", tc.name, got, parked, tc.want, tc.parked)
		}
	}
}

// A Work Brain parked on the person with nothing asked goes back to Brain
// after two hours: its wait ends and the Host gets the event once (owner
// 2026-11-19: such a Work is never shown to the person).
func TestNudgeHandsAParkedWorkBackToBrainOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host": {ID: "host", Hidden: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	item, err := store.CreateWork(Work{Title: "MewlaScreenVideo", Objective: "ship the H.264 path"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := WorkWaiting
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: &waiting}); err != nil {
		t.Fatal(err)
	}
	sent := func() int {
		count := 0
		for _, call := range fw.sentCalls {
			if call.sessionID == "host" && strings.Contains(call.text, UndecidedEventKind) {
				count++
			}
		}
		return count
	}
	if got := service.nudgeUndecided(time.Now().Add(time.Hour)); got != 0 || sent() != 0 {
		t.Fatalf("an hour in, nothing is raised: delivered %d, sent %d", got, sent())
	}
	if got := service.nudgeUndecided(time.Now().Add(3 * time.Hour)); got != 1 || sent() != 1 {
		t.Fatalf("two hours in, Brain gets it once: delivered %d, sent %d", got, sent())
	}
	work, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if work.Wake != nil {
		t.Fatalf("the wait parked on the person ended: wake %+v", work.Wake)
	}
	if got := service.nudgeUndecided(time.Now().Add(4 * time.Hour)); got != 0 || sent() != 1 {
		t.Fatalf("never twice for one wait: delivered %d, sent %d", got, sent())
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

// A wait Brain names (-wait-for a Mac, a PR head, a Worker's next commit) and
// a Work a Trigger owns (PRLoopWatcher waits on firings indefinitely) are
// Brain's own long waits: the nudge never wakes Brain for them (Brain
// 2026-11-19).
func TestNudgeLeavesNamedAndTriggerWaitsAlone(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host": {ID: "host", Hidden: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	waiting := WorkWaiting
	named, err := store.CreateWork(Work{Title: "CiSessionStoreReview", Objective: "re-check the PR heads"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor := "new heads of freeride-aws#108 and looper-exec#37 to re-check"
	if _, err := service.BrainUpdateWork(named.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor}); err != nil {
		t.Fatal(err)
	}
	watcherWork, err := store.CreateWork(Work{Title: "PRLoopWatcher", Objective: "standing PR loop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BrainUpdateWork(watcherWork.ID, WorkUpdate{Status: &waiting}); err != nil {
		t.Fatal(err)
	}
	service.SetTriggerWork(func(workID string) bool { return workID == watcherWork.ID })
	if got := service.nudgeUndecided(time.Now().Add(48 * time.Hour)); got != 0 {
		t.Fatalf("a named wait and a Trigger's Work stay quiet: delivered %d", got)
	}
	for _, id := range []string{named.ID, watcherWork.ID} {
		if item, _ := store.Work(id); item.Wake == nil || item.Status != WorkWaiting {
			t.Fatalf("%s: its wait is left as Brain set it: %+v", id, item.Wake)
		}
	}
	// Without the Trigger, the same bare park goes back to Brain.
	service.SetTriggerWork(nil)
	if got := service.nudgeUndecided(time.Now().Add(48 * time.Hour)); got != 1 {
		t.Fatalf("the bare park is handed back: delivered %d", got)
	}
}
