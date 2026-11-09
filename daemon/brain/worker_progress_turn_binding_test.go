package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// Observed 2026-11-09: a Worker that keeps working after its turn ended
// (background shells/agents, monitors) or received several sends reported its
// ask with the turn ID from its most recent contract and was rejected, so the
// ask never reached Brain. These tests drive the store-level entry point the
// control app uses for every `worker progress`.

type progressBindingFixture struct {
	store     *Store
	workID    string
	sessionID string
	base      time.Time
	seq       int
}

func newProgressBindingFixture(t *testing.T) *progressBindingFixture {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "long running", Objective: "keep working after the turn ends",
		CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "released",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &progressBindingFixture{
		store: store, workID: item.ID, sessionID: "mewla-worker-test:%169",
		base: time.Now().UTC().Add(-time.Hour),
	}
}

func (f *progressBindingFixture) at() time.Time {
	f.seq++
	return f.base.Add(time.Duration(f.seq) * time.Second)
}

// send prepares a signal-protocol delegated prompt (a "delegated-turn contract").
func (f *progressBindingFixture) send(t *testing.T, turnID string) {
	t.Helper()
	candidate := delegatedSubmissionCandidate(f.workID, f.sessionID, turnID, "input "+turnID, f.at())
	candidate.SignalProtocol = true
	if _, created, err := f.store.PrepareInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare %s created=%v err=%v", turnID, created, err)
	}
}

func (f *progressBindingFixture) report(turnID, kind string) (watcher.TurnProgressResult, error) {
	f.seq++
	return f.store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: f.sessionID, TurnID: turnID, Class: watcher.EvidenceControl,
		Kind: kind, SourceID: "control\x00" + turnID + kind + string(rune('a'+f.seq)), At: f.at(),
		Summary: "ask " + kind, Attention: "user_input", EventKind: "needs_judgment",
	})
}

func (f *progressBindingFixture) lose(t *testing.T) {
	t.Helper()
	st, err := f.store.FSM().State(lifecycle.WorkID(f.workID))
	if err != nil || st.Attempt == nil {
		t.Fatalf("no attempt to lose: %+v err=%v", st, err)
	}
	if _, err := f.store.FSM().ReportTurnLost(st.ID, lifecycle.AttemptIdentity{
		SessionID: st.Attempt.SessionID, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation,
	}, "lease_expired_escalation"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SyncWorkProjection(f.workID); err != nil {
		t.Fatal(err)
	}
}

func (f *progressBindingFixture) attempt(t *testing.T) lifecycle.TurnToken {
	t.Helper()
	st, err := f.store.FSM().State(lifecycle.WorkID(f.workID))
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt == nil {
		return ""
	}
	return st.Attempt.TurnToken
}

// stall mirrors the production producer of the 2026-11-09 incident: the stall
// supervisor escalates a stopped turn, which releases the Attempt as lost and
// opens the turn_lost review (EscalateStalledTurn).
func (f *progressBindingFixture) stall(t *testing.T, turnID string) {
	t.Helper()
	if err := f.store.EscalateStalledTurn(watcher.TurnStall{
		WorkID: f.workID, SessionID: f.sessionID, TurnID: turnID,
		Reason: "stopped", Summary: "Worker stopped", At: f.at(),
	}); err != nil {
		t.Fatal(err)
	}
	st, err := f.store.FSM().State(lifecycle.WorkID(f.workID))
	if err != nil || st.Attempt != nil || st.Review == nil || st.Review.Reason != "turn_lost" {
		t.Fatalf("stall did not release the turn as lost: %+v err=%v", st, err)
	}
}

func (f *progressBindingFixture) admitted(t *testing.T, turnID string) {
	t.Helper()
	f.send(t, turnID)
	if r, err := f.report(turnID, "running"); err != nil || !r.Matched {
		t.Fatalf("admit %s: %+v %v", turnID, r, err)
	}
}

// requireAskReachedBrain asserts the Worker's ask is the Work's open review
// and an actionable Work event, not merely that the call returned Matched.
func (f *progressBindingFixture) requireAskReachedBrain(t *testing.T, turnID string) {
	t.Helper()
	st, err := f.store.FSM().State(lifecycle.WorkID(f.workID))
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempt == nil || st.Attempt.TurnToken != lifecycle.TurnToken(turnID) || st.Attempt.SessionID != f.sessionID {
		t.Fatalf("attempt = %+v, want %s on %s", st.Attempt, turnID, f.sessionID)
	}
	if st.Review == nil || st.Review.Reason != "session.needs_input" {
		t.Fatalf("open review = %+v, want the Worker's ask (session.needs_input), not the stale turn_lost", st.Review)
	}
	event, found := turnEvent(t, f.store, f.workID, sessionTurnEventDedupeKey(f.sessionID, turnID, "session.needs_input"))
	if !found || event.Summary != "ask attention" || event.EventKind != "needs_judgment" {
		t.Fatalf("ask event = %+v found=%v", event, found)
	}
	// The Work's open review is what wakes Brain; it must be the ask's own
	// Event, not the loss that preceded it.
	work, err := f.store.Work(f.workID)
	if err != nil || work.Review == nil || !strings.Contains(work.Review.EventID, "attention") {
		t.Fatalf("projected Work review = %+v err=%v, want the ask's event", work.Review, err)
	}
	turn, found, err := f.store.TurnByID(f.sessionID, turnID)
	if err != nil || !found || turn.Status != watcher.TurnBlocked {
		t.Fatalf("turn row = %+v found=%v err=%v", turn, found, err)
	}
}

// 2026-11-09 %169: the stall supervisor released the latest turn as lost while
// the Worker kept working on background agents. Every later report with that
// turn's own ID was refused, so its asks never became Work events.
func TestWorkerAskOnAStalledTurnRevivesItAndReachesBrain(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.admitted(t, "turn:one")
	f.stall(t, "turn:one")

	result, err := f.report("turn:one", "attention")
	if err != nil || !result.Matched || result.Rejection != "" {
		t.Fatalf("ask on the latest turn = %+v err=%v", result, err)
	}
	f.requireAskReachedBrain(t, "turn:one")
}

func TestWorkerAskAfterBrainResolvedTheLossReviewReachesBrain(t *testing.T) {
	for _, disposition := range []WorkDisposition{WorkDispositionContinue, WorkDispositionWait} {
		t.Run(string(disposition), func(t *testing.T) {
			f := newProgressBindingFixture(t)
			f.admitted(t, "turn:one")
			f.stall(t, "turn:one")
			claimAndDeliverTestReview(t, f.store, "brain-host:@loss")
			lease := requireReviewDelivered(t, f.store, f.workID)
			request := WorkReviewDispositionRequest{
				WorkID: f.workID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
				ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: disposition,
			}
			if disposition == WorkDispositionWait {
				request.Wake = &WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(f.sessionID, "turn:one")}
			}
			if _, _, err := f.store.ResolveWorkReview(request); err != nil {
				t.Fatal(err)
			}
			if result, err := f.report("turn:one", "attention"); err != nil || !result.Matched {
				t.Fatalf("ask after resolved loss = %+v err=%v", result, err)
			}
			f.requireAskReachedBrain(t, "turn:one")
		})
	}
}

// A revived turn is a new execution generation: if it stops again Brain must be
// told again, not silently deduped against the first loss.
func TestRevivedTurnThatStopsAgainOpensANewLossReview(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.admitted(t, "turn:one")
	f.stall(t, "turn:one")
	if result, err := f.report("turn:one", "running"); err != nil || !result.Matched {
		t.Fatalf("revive by heartbeat = %+v err=%v", result, err)
	}
	st, _ := f.store.FSM().State(lifecycle.WorkID(f.workID))
	if st.Attempt == nil || st.Review != nil {
		t.Fatalf("heartbeat revival left attempt=%+v review=%+v", st.Attempt, st.Review)
	}
	first := st.Attempt.Generation
	f.stall(t, "turn:one")
	st, _ = f.store.FSM().State(lifecycle.WorkID(f.workID))
	if st.Fence <= first {
		t.Fatalf("fence did not advance: %d <= %d", st.Fence, first)
	}
	// The done report after the second loss still settles the turn.
	if result, err := f.report("turn:one", "done"); err != nil || !result.Matched {
		t.Fatalf("done after second loss = %+v err=%v", result, err)
	}
}

func TestOlderTurnOfTheSameSessionIsRedirectedToTheCurrentTurn(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.admitted(t, "turn:one")
	f.stall(t, "turn:one")
	f.admitted(t, "turn:two") // Brain sent a fresh prompt after the loss
	if got := f.attempt(t); got != "turn:two" {
		t.Fatalf("attempt = %s", got)
	}

	result, err := f.report("turn:one", "attention")
	if err != nil || !result.Matched || !strings.Contains(result.Notice, "turn:two") {
		t.Fatalf("older-turn ask = %+v err=%v, want redirect notice naming turn:two", result, err)
	}
	f.requireAskReachedBrain(t, "turn:two")

	// A heartbeat or terminal report from the older turn must neither clear the
	// newer turn's ask nor settle it; each is refused with the turn to use.
	for _, kind := range []string{"running", "done"} {
		before, _ := f.store.FSM().State(lifecycle.WorkID(f.workID))
		result, err = f.report("turn:one", kind)
		after, _ := f.store.FSM().State(lifecycle.WorkID(f.workID))
		if err != nil || result.Matched || !strings.Contains(result.Rejection, "--turn-id turn:two") {
			t.Fatalf("older-turn %s = %+v err=%v, want refusal naming turn:two", kind, result, err)
		}
		if after.Revision != before.Revision || after.Attempt == nil || after.Attempt.TurnToken != "turn:two" ||
			after.Review == nil || after.Review.Reason != "session.needs_input" {
			t.Fatalf("older %s crossed the fence: before=%d after=%+v", kind, before.Revision, after)
		}
	}
}

func TestRejectedProgressReportSaysWhyAndWhatToUse(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.admitted(t, "turn:one")

	result, err := f.report("turn:bogus", "running")
	if err != nil || result.Matched || !result.Owned ||
		!strings.Contains(result.Rejection, "turn:bogus") || !strings.Contains(result.Rejection, "--turn-id turn:one") {
		t.Fatalf("unknown turn = %+v err=%v", result, err)
	}
	result, err = f.report("", "running")
	if err != nil || result.Matched || !strings.Contains(result.Rejection, "--turn-id turn:one") {
		t.Fatalf("missing turn = %+v err=%v", result, err)
	}

	// Turn settled by the Worker's own done: a heartbeat afterwards is refused
	// with the reason, not as a bare identity mismatch.
	if result, err := f.report("turn:one", "done"); err != nil || !result.Matched {
		t.Fatalf("done = %+v err=%v", result, err)
	}
	result, err = f.report("turn:one", "running")
	if err != nil || result.Matched || !strings.Contains(result.Rejection, "turn:one") ||
		!strings.Contains(result.Rejection, "ended") {
		t.Fatalf("report after done = %+v err=%v", result, err)
	}
}

// Brain's newer prompt is queued but not started: reviving the old turn would
// take the slot the prompt is about to claim, so the old turn stays released and
// the Worker is told so.
func TestStalledTurnIsNotRevivedOverAQueuedNewerPrompt(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.admitted(t, "turn:one")
	f.stall(t, "turn:one")
	f.send(t, "turn:two")
	result, err := f.report("turn:one", "attention")
	if err != nil || result.Matched || !strings.Contains(result.Rejection, "turn:two") {
		t.Fatalf("ask over queued prompt = %+v err=%v", result, err)
	}
	if got := f.attempt(t); got != "" {
		t.Fatalf("queued prompt's slot was taken by %s", got)
	}
	// The newer prompt then starts normally and owns the Work.
	if r, err := f.report("turn:two", "running"); err != nil || !r.Matched {
		t.Fatalf("queued prompt admission = %+v err=%v", r, err)
	}
	if got := f.attempt(t); got != "turn:two" {
		t.Fatalf("attempt = %s", got)
	}
}
