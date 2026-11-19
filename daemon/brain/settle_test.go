package brain

import (
	"context"
	"errors"
	"os"
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

func TestStandingWorkIsNeverJudged(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "Looks finished."}}
	store, service, item := endedHandlingFixture(t, judge)
	service.SetStandingWork(func(workID string) bool { return workID == item.ID })
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled || len(judge.briefs) != 0 {
		t.Fatalf("a standing Work was judged: %+v briefs=%d", got, len(judge.briefs))
	}
	service.SetStandingWork(nil)
	runSettle(t, service)
	if len(judge.briefs) != 1 {
		t.Fatalf("an ordinary Work was not judged: %d", len(judge.briefs))
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
	service := newTestService(t, store, &fakeWatcher{}, nil)
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

// A Worker that left a page open on its display for the person (a sign-in)
// keeps its Work from the judge, however it reported its turn: closing the
// Work would close its Session and the page with it.
func TestAnOpenPageOnAWorkersDisplayKeepsTheWorkFromTheJudge(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone}}
	store, service, item := endedHandlingFixture(t, judge)
	var asked []string
	open := true
	service.SetOpenScreens(func(session string) bool {
		asked = append(asked, session)
		return open && session == "worker"
	})
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || closedByRuntime(t, store, item.ID) || len(judge.briefs) != 0 {
		t.Fatalf("a Work whose Worker has a page open was judged or closed: %+v asked=%v", got, asked)
	}
	if !strings.Contains(strings.Join(asked, ","), "worker") {
		t.Fatalf("the Work's Worker Session was not asked about: %v", asked)
	}
	// The person closed the page (or it went idle): the judge decides again.
	open = false
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status != WorkDone {
		t.Fatalf("with no page open the done verdict did not apply: %+v", got)
	}
}

func TestJudgeFailureClosesAResultBrainRead(t *testing.T) {
	// The judge cannot answer (an upstream 403, a kill, a rate limit): the
	// promise in AGENTS.md holds instead. Brain read the result and left no
	// turn, wait, question or prompt, so the Work closes, reopenably.
	judge := &fakeJudge{err: errors.New("headless judge: the model route http://127.0.0.1:3427 answered HTTP 403: Insufficient account balance")}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	got, err := store.Work(item.ID)
	if err != nil || got.Status != WorkDone || !closedByRuntime(t, store, item.ID) {
		t.Fatalf("a read result stayed open when the judge failed: %+v err=%v", got, err)
	}
	reason := ""
	for _, event := range mustWorkEvents(t, store, item.ID) {
		if event.Kind == "brain.work_closed" {
			reason = event.Summary
		}
	}
	if !strings.Contains(reason, "could not answer") || !strings.Contains(reason, "403") {
		t.Fatalf("the close does not say why the judge was not used: %q", reason)
	}
	if reopened, err := store.ReopenSettledWork(item.ID, "Brain updated the Work."); err != nil || !reopened {
		t.Fatalf("a fallback close is not undoable: %v %v", reopened, err)
	}
}

func TestOneJudgeFailureStandsForEveryWorkUntilTheRetry(t *testing.T) {
	judge := &fakeJudge{err: errors.New("signal: killed")}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	if len(judge.briefs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(judge.briefs))
	}
	if reopened, err := store.ReopenSettledWork(item.ID, "Brain updated the Work."); err != nil || !reopened {
		t.Fatalf("reopen: %v %v", reopened, err)
	}
	later := service.nowUTC().Add(settleDebounce + time.Minute)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if len(judge.briefs) != 1 {
		t.Fatalf("a judge that just failed was asked again at once (%d calls)", len(judge.briefs))
	}
}

func TestJudgeFailureLeavesAWorkWithoutAReadResultAlone(t *testing.T) {
	judge := &fakeJudge{err: errors.New("model unavailable")}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "plan", Objective: "plan the release"})
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(t, store, nil, nil)
	service.SetWorkJudge(judge)
	later := time.Now().Add(settleDebounce + time.Minute)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("a Work Brain never read a result of was closed: %+v", got)
	}
}

func TestKeepVerdictIsAWaitingStateWithTheJudgesReason(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "owner decision on the rate-limit options"}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	got, err := store.Work(item.ID)
	if err != nil || got.Status != WorkWaiting || got.WaitFor != "owner decision on the rate-limit options" ||
		got.Kept == nil || got.Kept.Reason != got.WaitFor {
		t.Fatalf("kept Work = %+v err=%v, want waiting on the judge's words", got, err)
	}
	stale := service.staleList(nil)
	if stale == nil || len(stale.Work) != 1 || stale.Work[0].ID != item.ID || stale.Work[0].Stale != StaleKept ||
		stale.Work[0].WaitFor != "owner decision on the rate-limit options" {
		t.Fatalf("stale list = %+v, want the kept Work", stale)
	}
}

func TestKeptWorkIsJudgedAgainOnANewEventAndDaily(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "A reply is still expected."}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	runSettle(t, service)
	if len(judge.briefs) != 1 {
		t.Fatalf("an unchanged kept Work was judged %d times", len(judge.briefs))
	}
	// Something happens on the Work: the Session it ran in went away.
	if _, err := store.appendWorkAuditEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.uncertain", DedupeKey: "test:session-ended", PayloadRef: "work:" + item.ID,
		SourceName: "worker", Summary: "Delegated Session ended", CreatedAt: service.nowUTC().Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	later := service.nowUTC().Add(2 * time.Second)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if len(judge.briefs) != 2 {
		t.Fatalf("a new event did not bring a new judgment (%d calls)", len(judge.briefs))
	}
	runSettle(t, service)
	if len(judge.briefs) != 2 {
		t.Fatalf("a kept Work was judged again with nothing new (%d calls)", len(judge.briefs))
	}
	nextDay := later.Add(keptRejudgeAfter + time.Minute)
	service.now = func() time.Time { return nextDay }
	runSettle(t, service)
	if len(judge.briefs) != 3 {
		t.Fatalf("a kept Work was not judged again after a day (%d calls)", len(judge.briefs))
	}
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("time closed a kept Work: %+v", got)
	}
}

func TestKeptWorkIsNeverClosedByAJudgeFailure(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "owner decision pending"}}
	store, service, item := endedHandlingFixture(t, judge)
	runSettle(t, service)
	judge.verdict, judge.err = JudgeVerdict{}, errors.New("headless judge timed out after 1m30s")
	nextDay := service.nowUTC().Add(keptRejudgeAfter + time.Minute)
	service.now = func() time.Time { return nextDay }
	runSettle(t, service)
	if len(judge.briefs) != 2 {
		t.Fatalf("the kept Work was not judged again after a day (%d calls)", len(judge.briefs))
	}
	if got, _ := store.Work(item.ID); got.Status != WorkWaiting || closedByRuntime(t, store, item.ID) {
		t.Fatalf("a judge failure closed a kept Work: %+v", got)
	}
}

func TestJudgeRunErrorNamesTheRouteAndTheUpstreamAnswer(t *testing.T) {
	dir := t.TempDir()
	settings := dir + "/settings.json"
	if err := os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:3427"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	route := judgeRoute(settings)
	err := judgeRunError(context.Background(), errors.New("exit status 1"),
		"Failed to authenticate. API Error: 403 Insufficient account balance", route)
	if !strings.Contains(err.Error(), "http://127.0.0.1:3427") || !strings.Contains(err.Error(), "HTTP 403: Insufficient account balance") {
		t.Fatalf("judge error = %v", err)
	}
	t.Setenv("ANTHROPIC_BASE_URL", "")
	if judgeRoute(dir+"/missing.json") != "Claude Code's own sign-in" {
		t.Fatal("a missing settings file should name Claude Code's own sign-in")
	}
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:42403/r/rt_x")
	if got := judgeRoute(dir + "/missing.json"); !strings.Contains(got, "http://127.0.0.1:42403/r/rt_x") {
		t.Fatalf("the daemon environment's route is not named: %q", got)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := judgeRunError(ctx, errors.New("signal: killed"), "", route); !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a timeout reads %v", err)
	}
}

func TestStaleListNamesDelegatedSessionsLeftWithoutOpenWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(t, store, nil, nil)
	now := time.Now()
	service.now = func() time.Time { return now }
	old := now.Add(-staleSessionAfter - time.Minute)
	stale := service.staleList([]WorkerRef{
		{ID: "mh-left", Name: "Left", Status: "unknown", Delegated: true, Updated: old},
		{ID: "mh-fresh", Delegated: true, Updated: now},
		{ID: "mh-host", Hidden: true, Delegated: true, Updated: old},
		{ID: "mh-user", Updated: old},
	})
	if stale == nil || len(stale.Sessions) != 1 || stale.Sessions[0].ID != "mh-left" {
		t.Fatalf("stale Sessions = %+v", stale)
	}
}
