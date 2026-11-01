package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A computer message is delivered under one receipt. The sender may deliver it
// again after the Brain conversation changed; that is the original admission,
// not a second provider submission, and it must be answered Accepted so the
// sender stops retrying.
func TestSubmitExternalUserInputDedupesReceiptAcrossThreadChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		hostID  = "brain-host:@computer-replay"
		oldID   = "thread-before"
		newID   = "thread-after"
		receipt = "computer:out:peer:m_replayed:1"
		body    = "<mewla_computer_message id=\"m_replayed\">\nhello\n</mewla_computer_message>"
	)
	if err := store.SetChatState(ChatState{ThreadID: oldID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Worker{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "generation"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	service := NewService(store, fw, nil)
	if disposition, err := service.SubmitExternalUserInput(receipt, body); err != nil || disposition != ExternalInputAccepted {
		t.Fatalf("first delivery disposition=%q err=%v", disposition, err)
	}
	if err := store.SetChatState(ChatState{ThreadID: newID}); err != nil {
		t.Fatal(err)
	}
	disposition, err := service.SubmitExternalUserInput(receipt, body)
	if err != nil || disposition != ExternalInputAccepted {
		t.Fatalf("replay on the new thread disposition=%q err=%v", disposition, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("replayed receipt reached the provider again: %+v", fw.sentCalls)
	}
	if _, found, err := store.BrainInputAdmission(receipt, newID); err != nil || found {
		t.Fatalf("replay created an admission on the new thread: found=%v err=%v", found, err)
	}
	if items, err := store.ThreadTimeline(oldID, 0); err != nil || len(items) != 1 || items[0].ID != receipt {
		t.Fatalf("original timeline=%+v err=%v", items, err)
	}

	// The same receipt carrying other text is a different input, never a replay.
	if disposition, err := service.SubmitExternalUserInput(receipt, body+"\nchanged"); err == nil || disposition != ExternalInputNotSubmitted {
		t.Fatalf("different input under one receipt: disposition=%q err=%v", disposition, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("different input reached the provider: %+v", fw.sentCalls)
	}
}

// acceptedReplayTestAdmission accepts without Prepare, as an older direct
// caller did: it is how the pre-fix state, one receipt accepted on two threads,
// is seeded now that Prepare refuses to create it.
func acceptedReplayTestAdmission(t *testing.T, store *Store, requestID, threadID, sessionID, body string, at time.Time) BrainInputAdmission {
	t.Helper()
	store.now = func() time.Time { return at }
	candidate := BrainInputAdmission{
		RequestID: requestID, ThreadID: threadID, HostSessionID: "brain-host:@replay",
		SessionID: sessionID, DisplayBody: body,
	}
	accepted, _, _, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}

// The live defect: one receipt admitted on two threads left the second without
// a presentation row, and every startup pass failed on it forever. A replay of
// the same input is already projected; a different input holding the identity
// is reported once; neither may stop the admissions or steps behind it.
func TestStartupReconciliationSurvivesRepeatedAndConflictingAdmissions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	const (
		replayID   = "computer:out:peer:m_replay:1"
		conflictID = "computer:out:peer:m_conflict:1"
		laterID    = "computer:out:peer:m_later:1"
	)
	first := acceptedReplayTestAdmission(t, store, replayID, "thread-a", "session-a", "same text", base)
	if err := store.ProjectBrainInputAdmission(first); err != nil {
		t.Fatal(err)
	}
	// Re-admitted on another thread by the pre-fix code path.
	acceptedReplayTestAdmission(t, store, replayID, "thread-b", "session-b", "same text", base.Add(time.Hour))

	owner := acceptedReplayTestAdmission(t, store, conflictID, "thread-a", "session-a", "owner text", base.Add(2*time.Hour))
	if err := store.ProjectBrainInputAdmission(owner); err != nil {
		t.Fatal(err)
	}
	acceptedReplayTestAdmission(t, store, conflictID, "thread-b", "session-b", "other text", base.Add(3*time.Hour))

	acceptedReplayTestAdmission(t, store, laterID, "thread-b", "session-b", "later text", base.Add(4*time.Hour))

	service := NewService(store, &fakeWatcher{}, nil)
	if err := store.ProjectBrainInputAdmission(mustAdmission(t, store, replayID, "thread-b")); err != nil {
		t.Fatalf("replay projection must be a no-op: %v", err)
	}
	complete, err := service.ReconcileSignalSystemStartup(nil, 64)
	if !complete {
		t.Fatalf("pass incomplete: %v", err)
	}
	if err == nil || !OnlyCleanupRefusals(err) {
		t.Fatalf("a conflicting input must be reported as final, got %v", err)
	}
	items, err := store.ThreadTimeline("thread-b", 0)
	if err != nil || len(items) != 1 || items[0].ID != laterID {
		t.Fatalf("the admission behind the bad records was not projected: %+v err=%v", items, err)
	}
	if items, err := store.ThreadTimeline("thread-a", 0); err != nil || len(items) != 2 {
		t.Fatalf("original rows changed: %+v err=%v", items, err)
	}

	// With the conflict reported, the replay alone completes cleanly.
	clean, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ok := acceptedReplayTestAdmission(t, clean, replayID, "thread-a", "session-a", "same text", base)
	if err := clean.ProjectBrainInputAdmission(ok); err != nil {
		t.Fatal(err)
	}
	acceptedReplayTestAdmission(t, clean, replayID, "thread-b", "session-b", "same text", base.Add(time.Hour))
	if complete, err := NewService(clean, &fakeWatcher{}, nil).ReconcileSignalSystemStartup(nil, 64); !complete || err != nil {
		t.Fatalf("a replayed receipt must not leave the pass failing: complete=%v err=%v", complete, err)
	}
}

func mustAdmission(t *testing.T, store *Store, requestID, threadID string) BrainInputAdmission {
	t.Helper()
	admission, found, err := store.BrainInputAdmission(requestID, threadID)
	if err != nil || !found {
		t.Fatalf("admission %s/%s found=%v err=%v", requestID, threadID, found, err)
	}
	return admission
}
