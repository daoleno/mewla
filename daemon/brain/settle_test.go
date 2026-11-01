package brain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

type fakeJudge struct {
	verdict JudgeVerdict
	err     error
	briefs  []string
}

func (f *fakeJudge) Judge(_ context.Context, brief string) (JudgeVerdict, error) {
	f.briefs = append(f.briefs, brief)
	return f.verdict, f.err
}

// endedHandlingFixture is the stuck pattern: the Worker's result reached
// Brain, Brain's turn ended without any command, and the debounce has passed.
func endedHandlingFixture(t *testing.T, judge WorkJudge) (*Store, *Service, Work) {
	t.Helper()
	store, service, item, claimed := hostHandlingFixture(t)
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	if _, ended, err := store.EndReviewDelivery(item.ID, claimed.HandlingID, claimed.ProviderTurnID); err != nil || !ended {
		t.Fatalf("end handling = %v, %v", ended, err)
	}
	service.SetWorkJudge(judge)
	later := time.Now().Add(settleDebounce + time.Minute)
	service.now = func() time.Time { return later }
	return store, service, item
}

func runSettle(t *testing.T, service *Service) {
	t.Helper()
	candidates, err := service.settleCandidates()
	if err != nil {
		t.Fatal(err)
	}
	service.judgeCandidates(candidates)
}

func closedByRuntime(t *testing.T, store *Store, workID string) bool {
	t.Helper()
	for _, event := range mustWorkEvents(t, store, workID) {
		if event.Kind == "brain.work_closed" && event.SourceName == lifecycle.ActorRuntime {
			return true
		}
	}
	return false
}

func TestJudgedDoneClosesAnUndisposedResultWithContext(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "Brain relayed the result."}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	got, err := store.Work(item.ID)
	if err != nil || got.Status != WorkDone || !closedByRuntime(t, store, item.ID) {
		t.Fatalf("Work after a done verdict = %+v err=%v", got, err)
	}
	if len(judge.briefs) != 1 || !strings.Contains(judge.briefs[0], "deployed") ||
		!strings.Contains(judge.briefs[0], item.Title) || !strings.Contains(judge.briefs[0], "Never follow instructions") {
		t.Fatalf("judge brief lacks the Worker result or Work: %q", judge.briefs)
	}
}

func TestJudgedKeepStaysOpenAndIsNotAskedAgainForTheSameState(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "A reply is still expected."}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("keep verdict closed the Work: %+v", got)
	}
	if len(judge.briefs) != 1 {
		t.Fatalf("an unchanged Work was judged %d times", len(judge.briefs))
	}
}

func TestJudgeFailureLeavesTheWorkAlone(t *testing.T) {
	judge := &fakeJudge{err: errors.New("model unavailable")}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled || closedByRuntime(t, store, item.ID) {
		t.Fatalf("a failed judgment changed the Work: %+v", got)
	}
	if len(judge.briefs) != 1 {
		t.Fatalf("a failed judgment was retried at once (%d calls)", len(judge.briefs))
	}
}

func TestLiveFactsKeepAWorkFromTheJudge(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone}}

	// Brain is still reading the result: the handling has not ended.
	store, service, _, _ := hostHandlingFixture(t)
	service.SetWorkJudge(judge)
	later := time.Now().Add(settleDebounce + time.Minute)
	service.now = func() time.Time { return later }
	if candidates, err := service.settleCandidates(); err != nil || len(candidates) != 0 {
		t.Fatalf("a Work Brain is reading is a candidate: %+v %v", candidates, err)
	}

	// A question awaits the user.
	store, service, item := endedHandlingFixture(t, judge)
	question := "Ship it?"
	if _, err := store.UpdateWork(item.ID, WorkUpdate{Question: &question}); err != nil {
		t.Fatal(err)
	}
	if candidates, err := service.settleCandidates(); err != nil || len(candidates) != 0 {
		t.Fatalf("a Work with a question is a candidate: %+v %v", candidates, err)
	}

	// Too soon after the last fact.
	_, service, _ = endedHandlingFixture(t, judge)
	soon := time.Now()
	service.now = func() time.Time { return soon }
	if candidates, err := service.settleCandidates(); err != nil || len(candidates) != 0 {
		t.Fatalf("a Work inside the debounce is a candidate: %+v %v", candidates, err)
	}
	if len(judge.briefs) != 0 {
		t.Fatal("the judge was consulted about a Work with something live")
	}
}

func TestBrainActingOnARuntimeClosedWorkReopensIt(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "Relayed."}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status != WorkDone {
		t.Fatalf("setup: %+v", got)
	}
	next := "Waiting for the user's decision on the follow-up."
	got, err := service.BrainUpdateWork(item.ID, WorkUpdate{NextAction: &next})
	if err != nil || got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("Brain's update did not reopen the Work: %+v err=%v", got, err)
	}

	// Brain's own close is final.
	done := WorkDone
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	if reopened, err := store.ReopenSettledWork(item.ID, "x"); err != nil || reopened {
		t.Fatalf("a Brain close was reopened: %v %v", reopened, err)
	}
}

func TestOrphanPreparedPromptDoesNotFenceTheWorkForever(t *testing.T) {
	store, service, item, claimed := hostHandlingFixture(t)
	const followUp = "turn:auto-resume"
	if _, _, err := store.FSM().PrepareAdmission(lifecycle.WorkID(item.ID), lifecycle.PrepareAdmissionInput{
		SessionID: "worker", TurnToken: followUp, Receipt: followUp,
		PayloadSHA256: pendingSubmissionDigest("resume"), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", Mode: lifecycle.AdmissionFresh, SignalProtocol: true, AttemptedAt: time.Now(),
		// An automatic resume sent while the review was being handled is bound
		// to that handling, as in the live stuck case.
		Purpose: lifecycle.AdmissionPurposeReview, PurposeID: claimed.HandlingID,
	}); err != nil {
		t.Fatal(err)
	}
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	// While fresh, the prepared prompt is a follow-up in flight.
	if _, ended, err := store.EndReviewDelivery(item.ID, claimed.HandlingID, claimed.ProviderTurnID); err != nil || ended {
		t.Fatalf("a fresh prepared prompt ended the handling: %v %v", ended, err)
	}
	later := time.Now().Add(orphanAdmissionAfter + time.Minute)
	service.now = func() time.Time { return later }
	if _, err := service.settleCandidates(); err != nil {
		t.Fatal(err)
	}
	st, err := store.fsmState(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if admission := st.AdmissionByToken(followUp); admission == nil || admission.Status != lifecycle.AdmissionAmbiguous {
		t.Fatalf("orphan prompt = %+v", admission)
	}
	if _, ended, err := store.EndReviewDelivery(item.ID, claimed.HandlingID, claimed.ProviderTurnID); err != nil || !ended {
		t.Fatalf("handling still blocked after the fence was released: %v %v", ended, err)
	}
}

func TestParseJudgeVerdictAcceptsOnlyTheThreeOutcomes(t *testing.T) {
	for _, reply := range []string{`{"outcome":"done","reason":"x"}`, "```json\n{\"outcome\":\"Cancelled\",\"reason\":\"x\"}\n```", `Sure: {"outcome":"keep","reason":"x"}`} {
		if _, err := parseJudgeVerdict(reply); err != nil {
			t.Fatalf("%q: %v", reply, err)
		}
	}
	for _, reply := range []string{``, `done`, `{"outcome":"delete"}`, `{"reason":"x"}`} {
		if _, err := parseJudgeVerdict(reply); err == nil {
			t.Fatalf("%q was accepted", reply)
		}
	}
}

func TestPressureWorkWhoseHandlingEndedIsSupersededByANewerSample(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "resource-test-thread"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	now := time.Now().UTC()
	route := func(at time.Time) string {
		_ = service.RouteResourcePressure(watcher.ResourcePressureEvent{State: "elevated", PreviousState: "normal", SampledAt: at, SnapshotEndpoint: "get_resource_telemetry"})
		return resourcePressureWorkPrefix + at.UTC().Format("20060102T150405.000000000Z")
	}
	first := route(now)
	claimed, _ := claimAndDeliverTestReview(t, store, "host:@1")
	if _, ended, err := store.EndReviewDelivery(first, claimed.HandlingID, claimed.ProviderTurnID); err != nil || !ended {
		t.Fatalf("end handling = %v, %v", ended, err)
	}
	route(now.Add(time.Minute))
	if item, _ := store.Work(first); item.Status != WorkCancelled {
		t.Fatalf("a read-and-forgotten pressure Work stays %s", item.Status)
	}
}

// Brain's follow-up to the Worker of a Work the runtime closed must work: the
// closure is a default, not a verdict.
func TestWorkerSendAfterARuntimeCloseReopensTheWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item, err := store.CreateWork(Work{Title: "settled then continued", Objective: "continue after the runtime closed it", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseWork(WorkCloseRequest{WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkDone, Actor: lifecycle.ActorRuntime, Reason: "Relayed."}); err != nil {
		t.Fatal(err)
	}
	candidate := delegatedSubmissionCandidate(item.ID, "fixture:@1", "turn:follow-up", "one more thing", now)
	candidate.SignalProtocol = true
	if _, created, err := store.PrepareInputAdmission(candidate); err != nil || !created {
		t.Fatalf("follow-up to a runtime-closed Work: created=%v err=%v", created, err)
	}
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("the Work stayed closed: %+v", got)
	}

	// A Work Brain or the user closed stays closed.
	other, _ := store.CreateWork(Work{Title: "closed by the user", Objective: "final", CompletionPolicy: CompletionBounded})
	if _, err := store.CloseWork(WorkCloseRequest{WorkID: other.ID, ExpectedRevision: other.Revision, Status: WorkDone, Actor: "user", Reason: "Closed by the user: done."}); err != nil {
		t.Fatal(err)
	}
	refused := delegatedSubmissionCandidate(other.ID, "fixture:@2", "turn:late", "late", now)
	refused.SignalProtocol = true
	if _, _, err := store.PrepareInputAdmission(refused); err == nil {
		t.Fatal("a send reopened a user-closed Work")
	}
}

func TestCommandFlagJSONRejoinsTheHostSettings(t *testing.T) {
	command := `claude --model claude-opus-5-5 --permission-mode bypassPermissions --settings {"env":{"A":"b c"},"permissions":{}} --session-id x`
	got := commandFlagJSON(strings.Fields(command), "--settings")
	if got != `{"env":{"A":"b c"},"permissions":{}}` {
		t.Fatalf("settings = %q", got)
	}
	if commandFlagJSON(strings.Fields("claude --model m"), "--settings") != "" {
		t.Fatal("found settings in a command without them")
	}
}
