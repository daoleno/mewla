package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

func TestTurnRecoveryRecordsSurviveRestartAndStallEscalatesOnce(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "long render", Objective: "render the trailer"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host":   {ID: "host", Hidden: true, State: classifier.StateDone},
		"worker": {ID: "worker", Delegated: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	if _, err := fw.SubmitDelegatedWorkInput("worker", "render the trailer", item.ID, "turn:first", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "turn:first", Class: watcher.EvidenceControl, Kind: "running", SourceID: "running-first", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	workID, records, found, err := service.TurnRecoveries("worker", "turn:first")
	if err != nil || !found || workID != item.ID || len(records) != 0 {
		t.Fatalf("live attempt recoveries = (%q, %+v, %v, %v)", workID, records, found, err)
	}
	if _, _, found, _ := service.TurnRecoveries("worker", "turn:other"); found {
		t.Fatal("a different Turn reported as the live Attempt")
	}
	base := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	for attempt := 1; attempt <= 2; attempt++ {
		record := watcher.TurnRecovery{WorkID: item.ID, SessionID: "worker", FromTurnID: "turn:first", TurnID: "turn:resume-" + string(rune('0'+attempt)),
			Attempt: attempt, Reason: "server_error: API Error: Connection lost mid-response.", Outcome: watcher.InputAccepted, At: base.Add(time.Duration(attempt) * time.Minute)}
		if err := service.RecordTurnRecovery(record); err != nil {
			t.Fatal(err)
		}
		if err := service.RecordTurnRecovery(record); err != nil { // dedupe
			t.Fatal(err)
		}
	}
	// A daemon restart reads the same budget.
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw.turnStore = store
	service = NewService(store, fw, nil)
	_, records, found, err = service.TurnRecoveries("worker", "turn:first")
	if err != nil || !found || len(records) != 2 || records[0].Attempt != 1 || records[1].Attempt != 2 ||
		records[1].TurnID != "turn:resume-2" || records[1].Outcome != watcher.InputAccepted {
		t.Fatalf("records after restart = %+v (%v, %v)", records, found, err)
	}

	stall := watcher.TurnStall{WorkID: item.ID, SessionID: "worker", TurnID: "turn:first", Reason: "retries_exhausted", Attempts: 5,
		Summary: "Worker stalled: provider error persisted after 5 automatic resumes (server_error: API Error: Connection lost mid-response.). Last output: ● Rendering",
		At:      base.Add(time.Hour)}
	if err := service.EscalateStalledTurn(stall); err != nil {
		t.Fatal(err)
	}
	state, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempt != nil || state.Review == nil || state.Review.Reason != "turn_lost" || state.LastSummary != "worker_stalled: retries_exhausted" {
		t.Fatalf("state after stall = attempt %+v review %+v summary %q", state.Attempt, state.Review, state.LastSummary)
	}
	// Repeated escalation of the same stall changes nothing: one Brain event.
	for i := 0; i < 2; i++ {
		if err := service.EscalateStalledTurn(stall); err != nil {
			t.Fatal(err)
		}
	}
	again, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if again.Revision != state.Revision || again.Review.EventID != state.Review.EventID {
		t.Fatalf("repeated escalation changed Work: rev %d->%d review %s->%s", state.Revision, again.Revision, state.Review.EventID, again.Review.EventID)
	}
	stallRows := 0
	for _, event := range mustWorkEvents(t, store, item.ID) {
		if event.Kind == workEventKindStalled {
			stallRows++
		}
	}
	if stallRows != 1 {
		t.Fatalf("stall evidence rows = %d, want 1", stallRows)
	}
	if _, _, found, _ := service.TurnRecoveries("worker", "turn:first"); found {
		t.Fatal("released Attempt still reported live")
	}
	action, found, err := store.ClaimNextReviewAction("host")
	if err != nil || !found {
		t.Fatalf("claim = %+v %v %v", action, found, err)
	}
	if action.WorkID != item.ID || action.Kind != "turn_lost" || !strings.Contains(action.Summary, "after 5 automatic resumes") ||
		!strings.Contains(action.Summary, "Last output: ● Rendering") {
		t.Fatalf("Brain receives %+v", action)
	}
}

func mustWorkEvents(t *testing.T, store *Store, workID string) []WorkEvent {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadPresentationLocked()
	if err != nil {
		t.Fatal(err)
	}
	out := []WorkEvent{}
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID {
			out = append(out, event)
		}
	}
	return out
}

// A drop before the Worker's first progress report leaves only the prepared
// prompt; Brain still hears once, as unknown delivery, never a replay.
func TestStallBeforeFirstProgressOpensOneUnknownDeliveryReview(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "early drop", Objective: "start"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host":   {ID: "host", Hidden: true, State: classifier.StateDone},
		"worker": {ID: "worker", Delegated: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	// Delivered by transport; the Worker never reported progress.
	if _, _, err := store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: item.ID, SessionID: "worker", ProposedTurnID: "turn:initial", Receipt: "turn:initial",
		PayloadSHA256: pendingSubmissionDigest("start"), ProcessIdentity: "delegated-process",
		PaneGeneration: "delegated-pane", AcceptedAt: time.Now().UTC(), Mode: watcher.InputAdmissionFresh,
		SignalProtocol: true,
	}); err != nil {
		t.Fatal(err)
	}
	workID, _, found, err := service.TurnRecoveries("worker", "turn:initial")
	if err != nil || !found || workID != item.ID {
		t.Fatalf("unadmitted prompt recoveries = (%q, %v, %v)", workID, found, err)
	}
	if err := service.MarkInputAdmissionAmbiguous("worker", "turn:initial", "provider idle"); err != nil {
		t.Fatal(err)
	}
	stall := watcher.TurnStall{WorkID: item.ID, SessionID: "worker", TurnID: "turn:initial", Reason: "provider_error",
		Summary: "Worker stalled: provider error needs attention; not resumed automatically (authentication_failed: Not logged in)", At: time.Now()}
	for i := 0; i < 3; i++ {
		if err := service.EscalateStalledTurn(stall); err != nil {
			t.Fatal(err)
		}
	}
	state, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if state.Review == nil || state.Review.Reason != "submission_ambiguous" || state.Review.Ref != "turn:initial" || state.Attempt != nil {
		t.Fatalf("state = review %+v attempt %+v", state.Review, state.Attempt)
	}
	action, found, err := store.ClaimNextReviewAction("host")
	if err != nil || !found || action.Kind != "submission_ambiguous" || !strings.Contains(action.Summary, "Not logged in") {
		t.Fatalf("Brain receives %+v (%v %v)", action, found, err)
	}
}
