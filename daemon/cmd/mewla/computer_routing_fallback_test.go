package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// admittingComputerWatcher submits a delegated follow-up through the Brain
// turn ledger the way the real watcher does: a follow-up without a Work is
// admitted only when some open Work's Attempt owns the Session.
type admittingComputerWatcher struct {
	*fakeControlWatcher
	// liveTurn is a Session's running Turn, which a follow-up steers.
	liveTurn map[string]string
}

func (w *admittingComputerWatcher) SubmitDelegatedInput(sessionID, payload, turnID string, acceptedAt time.Time) (watcher.InputResult, error) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	candidate := watcher.InputAdmission{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: digest, ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}
	if live := w.liveTurn[sessionID]; live != "" {
		candidate.Mode, candidate.ExistingTurnID, candidate.BaselineActivityID = watcher.InputAdmissionConditionalSteer, live, "activity-fixture"
	}
	if _, _, err := w.turnStore.PrepareInputAdmission(candidate); err != nil {
		result := watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: turnID, TurnID: turnID}
		return result, &watcher.InputSubmissionError{Result: result, Cause: err}
	}
	return w.fakeControlWatcher.SubmitDelegatedInput(sessionID, payload, turnID, acceptedAt)
}

type computerFallbackFixture struct {
	fw    *admittingComputerWatcher
	store *brain.Store
	app   *controlApp
}

func newComputerFallbackFixture(t *testing.T) computerFallbackFixture {
	t.Helper()
	fw := &admittingComputerWatcher{fakeControlWatcher: newFakeControlWatcher(), liveTurn: map[string]string{}}
	store := newControlBrainStore(t)
	fw.turnStore = store
	fw.workers["host"] = &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateRunning}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread"}); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{watcher: fw, brainStore: store, brainService: brain.NewService(store, fw, nil)}
	return computerFallbackFixture{fw: fw, store: store, app: app}
}

func (f computerFallbackFixture) closedWork(t *testing.T, actor string) brain.Work {
	t.Helper()
	item, err := f.store.CreateWork(brain.Work{Title: "asked another computer", Objective: "read numbers", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.FSM().Complete(lifecycle.WorkID(item.ID), 0, actor, "finished before the reply"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	return item
}

func (f computerFallbackFixture) sentToBrain() int {
	count := 0
	for _, sent := range f.fw.sent {
		if sent.id == "host" {
			count++
		}
	}
	return count
}

func (f computerFallbackFixture) submittedTo(id string) int {
	count := 0
	for _, sent := range f.fw.submitted {
		if sent.id == id {
			count++
		}
	}
	return count
}

func computerReply(origin computers.Origin) computers.Delivery {
	return computers.Delivery{
		Identity: "computer:out:peer:m_reply", Receipt: "computer:out:peer:m_reply:1",
		Body: "<computer-message from=\"mac\">numbers</computer-message>", Origin: origin,
	}
}

// The 2026-11-23 case: a Calendar run's agent asked, finished its run and
// stayed open with no Work owning it. Its reply was refused as "no active
// Brain Work owns delegated Session" on every attempt.
func TestComputerReplyToSessionWithoutWorkReachesBrain(t *testing.T) {
	f := newComputerFallbackFixture(t)
	f.fw.workers["calendar-agent"] = &classifier.Worker{ID: "calendar-agent", Delegated: true, Command: "claude"}
	d := computerReply(computers.Origin{SessionID: "calendar-agent"})
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := f.app.deliverComputerMessage(d)
		if err != nil || result != computers.DispositionAccepted {
			t.Fatalf("attempt %d: result=%s err=%v", attempt, result, err)
		}
	}
	if f.submittedTo("calendar-agent") != 0 {
		t.Fatalf("a Session no Work owns received the reply: %+v", f.fw.submitted)
	}
	if f.sentToBrain() != 1 {
		t.Fatalf("Brain received %d, want 1: %+v", f.sentToBrain(), f.fw.sent)
	}
}

func TestComputerReplyToClosedWorkWithPresentSessionLandsOnce(t *testing.T) {
	for _, actor := range []string{"brain", lifecycle.ActorRuntime} {
		t.Run(actor, func(t *testing.T) {
			f := newComputerFallbackFixture(t)
			f.fw.workers["%7"] = &classifier.Worker{ID: "%7", Delegated: true, Command: "codex"}
			item := f.closedWork(t, actor)
			d := computerReply(computers.Origin{SessionID: "%7", WorkID: item.ID})
			result, err := f.app.deliverComputerMessage(d)
			if err != nil || result != computers.DispositionAccepted {
				t.Fatalf("result=%s err=%v", result, err)
			}
			events, _ := f.store.ListWorkEvents(item.ID)
			landed := f.submittedTo("%7") + f.sentToBrain()
			if actor == lifecycle.ActorRuntime {
				// A runtime-closed Work reopens for a follow-up: the asker takes it.
				if f.submittedTo("%7") != 1 || landed != 1 {
					t.Fatalf("runtime-closed: submitted=%+v sent=%+v", f.fw.submitted, f.fw.sent)
				}
				return
			}
			if f.submittedTo("%7") != 0 {
				t.Fatalf("a Session whose Work is closed received the reply: %+v", f.fw.submitted)
			}
			if f.sentToBrain() != 1 {
				t.Fatalf("Brain received %d, want 1 (events=%+v)", f.sentToBrain(), events)
			}
		})
	}
}

func TestComputerReplyToClosedWorkWithAbsentSessionReachesBrain(t *testing.T) {
	f := newComputerFallbackFixture(t)
	item := f.closedWork(t, "brain")
	d := computerReply(computers.Origin{SessionID: "gone", WorkID: item.ID})
	result, err := f.app.deliverComputerMessage(d)
	if err != nil || result != computers.DispositionAccepted {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if f.sentToBrain() != 1 {
		t.Fatalf("Brain received %d, want 1: %+v", f.sentToBrain(), f.fw.sent)
	}
	if !strings.Contains(f.fw.sent[len(f.fw.sent)-1].text, "numbers") {
		t.Fatalf("Brain input=%q", f.fw.sent[len(f.fw.sent)-1].text)
	}
}

func (f computerFallbackFixture) openWorkOwnedBy(t *testing.T, sessionID string) brain.Work {
	t.Helper()
	item, err := f.store.CreateWork(brain.Work{Title: "owns the asker", Objective: "keep running", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	f.fw.liveTurn[sessionID] = admitControlWorkOwner(t, f.store, item.ID, sessionID)
	return item
}

// Positive control: without an origin Work, a Session whose Attempt an open
// Work owns still takes the reply itself.
func TestComputerReplyToSessionOwnedByOpenWorkStaysWithSession(t *testing.T) {
	f := newComputerFallbackFixture(t)
	f.fw.workers["%8"] = &classifier.Worker{ID: "%8", Delegated: true, Command: "codex"}
	f.openWorkOwnedBy(t, "%8")
	result, err := f.app.deliverComputerMessage(computerReply(computers.Origin{SessionID: "%8"}))
	if f.submittedTo("%8") != 1 || f.sentToBrain() != 0 {
		t.Fatalf("result=%s err=%v submitted=%+v sent=%+v", result, err, f.fw.submitted, f.fw.sent)
	}
}

func TestComputerReplyOpenPromptWaitsOnlyForASessionThatCanTakeIt(t *testing.T) {
	prompt := &classifier.Prompt{ID: "perm"}
	t.Run("takes", func(t *testing.T) {
		f := newComputerFallbackFixture(t)
		f.fw.workers["%8"] = &classifier.Worker{ID: "%8", Delegated: true, Command: "codex", Prompt: prompt}
		f.openWorkOwnedBy(t, "%8")
		result, err := f.app.deliverComputerMessage(computerReply(computers.Origin{SessionID: "%8"}))
		if result != computers.DispositionNotSubmitted || err == nil || f.submittedTo("%8") != 0 || f.sentToBrain() != 0 {
			t.Fatalf("result=%s err=%v submitted=%+v sent=%+v", result, err, f.fw.submitted, f.fw.sent)
		}
	})
	t.Run("cannot take", func(t *testing.T) {
		f := newComputerFallbackFixture(t)
		f.fw.workers["calendar-agent"] = &classifier.Worker{ID: "calendar-agent", Delegated: true, Command: "claude", Prompt: prompt}
		result, err := f.app.deliverComputerMessage(computerReply(computers.Origin{SessionID: "calendar-agent"}))
		if result != computers.DispositionAccepted || err != nil || f.sentToBrain() != 1 {
			t.Fatalf("result=%s err=%v sent=%+v", result, err, f.fw.sent)
		}
	})
}

func TestComputerReplyToRuntimeClosedWorkWithAbsentSessionReachesBrain(t *testing.T) {
	f := newComputerFallbackFixture(t)
	item := f.closedWork(t, lifecycle.ActorRuntime)
	result, err := f.app.deliverComputerMessage(computerReply(computers.Origin{SessionID: "gone", WorkID: item.ID}))
	if err != nil || result != computers.DispositionAccepted || f.sentToBrain() != 1 {
		t.Fatalf("result=%s err=%v sent=%+v", result, err, f.fw.sent)
	}
}
