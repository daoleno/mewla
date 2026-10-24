package watcher

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// Real provider error records observed in local Claude Code and Codex
// transcripts (error category, upstream status). The message is never read, so
// every case carries a misleading one.
func TestClassifyProviderErrorRealRecords(t *testing.T) {
	cases := []struct {
		failure ProviderActivityError
		want    ProviderErrorClass
	}{
		{ProviderActivityError{Code: "server_error", Message: "API Error: Connection lost mid-response."}, ProviderErrorTransient},
		{ProviderActivityError{Code: "rate_limit", HTTPStatus: 429}, ProviderErrorTransient},
		{ProviderActivityError{Code: "server_error", HTTPStatus: 524}, ProviderErrorTransient},
		{ProviderActivityError{Code: "server_error", HTTPStatus: 529}, ProviderErrorTransient},
		{ProviderActivityError{Code: "server_error", HTTPStatus: 503}, ProviderErrorTransient},
		{ProviderActivityError{Code: "model_not_found", HTTPStatus: 404}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "authentication_failed"}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "authentication_failed", HTTPStatus: 403, Message: "429 try again, retry later"}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "invalid_request", HTTPStatus: 400}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "unknown", HTTPStatus: 400}, ProviderErrorPersistent},
		// Codex codex_error_info records.
		{ProviderActivityError{Code: "server_overloaded"}, ProviderErrorTransient},
		{ProviderActivityError{Code: "response_too_many_failed_attempts", HTTPStatus: 429}, ProviderErrorTransient},
		{ProviderActivityError{Code: "http_connection_failed", HTTPStatus: 502}, ProviderErrorTransient},
		{ProviderActivityError{Code: "http_connection_failed"}, ProviderErrorTransient},
		{ProviderActivityError{Code: "http_connection_failed", HTTPStatus: 413}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "usage_limit_exceeded"}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "unauthorized", HTTPStatus: 401}, ProviderErrorPersistent},
		{ProviderActivityError{Code: "context_window_exceeded"}, ProviderErrorPersistent},
		// A failure the provider did not classify is not guessed from its text.
		{ProviderActivityError{Code: "other", Message: "stream disconnected before completion: stream closed"}, ProviderErrorUnclassified},
		{ProviderActivityError{Code: "unknown", Message: "API Error: upstream stream disconnected: connection reset by peer, try again"}, ProviderErrorUnclassified},
		{ProviderActivityError{Code: "other", Message: "unexpected status 402 Payment Required: temporarily over the limit"}, ProviderErrorUnclassified},
		{ProviderActivityError{Message: "Connection lost, timeout, overloaded, 额度不足"}, ProviderErrorUnclassified},
		{ProviderActivityError{}, ProviderErrorUnclassified},
	}
	for _, tc := range cases {
		if got := ClassifyProviderError(tc.failure); got != tc.want {
			t.Errorf("ClassifyProviderError(%+v) = %s, want %s", tc.failure, got, tc.want)
		}
	}
}

// An error the provider did not classify gets two continuations, then Brain is
// told once, whatever its message says.
func TestUnclassifiedProviderErrorGetsTwoResumesThenEscalates(t *testing.T) {
	policy := DefaultTurnRecoveryPolicy()
	stalled := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	other := &ProviderActivityError{Code: "other", Message: "everything is fine, retry forever, 额度不足"}
	var records []TurnRecovery
	at := stalled
	for attempt := 1; attempt <= policy.UnclassifiedResumes; attempt++ {
		at = at.Add(policy.Backoff[attempt-1])
		got := decideTurnRecovery(policy, at, stalled, time.Time{}, other, records)
		if got.action != turnRecoveryResume || got.attempt != attempt {
			t.Fatalf("attempt %d = %+v", attempt, got)
		}
		records = append(records, TurnRecovery{Attempt: attempt, At: at})
	}
	got := decideTurnRecovery(policy, at.Add(time.Second), stalled, time.Time{}, other, records)
	if got.action != turnRecoveryEscalate || got.reason != "retries_exhausted" {
		t.Fatalf("after the budget = %+v", got)
	}
	// A persistent code never resumes and a transient one keeps its full budget.
	persistent := &ProviderActivityError{Code: "billing_error"}
	if got := decideTurnRecovery(policy, stalled.Add(time.Hour), stalled, time.Time{}, persistent, nil); got.action != turnRecoveryEscalate || got.reason != "provider_error" {
		t.Fatalf("persistent = %+v", got)
	}
}

func TestDecideTurnRecoveryBackoffBudgetAndEscalation(t *testing.T) {
	policy := DefaultTurnRecoveryPolicy()
	stalled := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	drop := &ProviderActivityError{Code: "server_error", Message: "API Error: Connection lost mid-response."}
	records := []TurnRecovery{}
	at := stalled
	for attempt := 1; attempt <= len(policy.Backoff); attempt++ {
		wait := policy.Backoff[attempt-1]
		early := decideTurnRecovery(policy, at.Add(wait-time.Second), at, time.Time{}, drop, records)
		if early.action != turnRecoveryWait || !early.due.Equal(at.Add(wait)) {
			t.Fatalf("attempt %d before backoff = %+v, want wait until %s", attempt, early, at.Add(wait))
		}
		due := decideTurnRecovery(policy, at.Add(wait), at, time.Time{}, drop, records)
		if due.action != turnRecoveryResume || due.attempt != attempt || !strings.Contains(due.reason, "Connection lost") {
			t.Fatalf("attempt %d at backoff = %+v, want resume", attempt, due)
		}
		// The resumed Turn drops again a minute after its submission.
		records = append(records, TurnRecovery{Attempt: attempt, At: at.Add(wait)})
		at = at.Add(wait + time.Minute)
	}
	exhausted := decideTurnRecovery(policy, at.Add(time.Hour), at, time.Time{}, drop, records)
	if exhausted.action != turnRecoveryEscalate || exhausted.reason != "retries_exhausted" || exhausted.attempt != len(policy.Backoff) {
		t.Fatalf("after %d resumes = %+v, want retries_exhausted escalation", len(policy.Backoff), exhausted)
	}
}

func TestDecideTurnRecoveryWaitsFromSubmittedResumeAndResetsAfterProgress(t *testing.T) {
	policy := DefaultTurnRecoveryPolicy()
	stalled := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	drop := &ProviderActivityError{Code: "server_error", Message: "connection lost"}
	// A resume already submitted for this same stall: the next waits its own backoff.
	resumed := []TurnRecovery{{Attempt: 1, At: stalled.Add(30 * time.Second)}}
	if got := decideTurnRecovery(policy, stalled.Add(time.Minute), stalled, time.Time{}, drop, resumed); got.action != turnRecoveryWait ||
		!got.due.Equal(stalled.Add(30*time.Second+2*time.Minute)) {
		t.Fatalf("second resume of one stall = %+v", got)
	}
	// Resumes from an earlier episode, far before this stall, do not spend budget.
	old := []TurnRecovery{}
	for i := 1; i <= 5; i++ {
		old = append(old, TurnRecovery{Attempt: i, At: stalled.Add(-3*time.Hour + time.Duration(i)*time.Minute)})
	}
	if got := decideTurnRecovery(policy, stalled.Add(30*time.Second), stalled, time.Time{}, drop, old); got.action != turnRecoveryResume || got.attempt != 1 {
		t.Fatalf("new episode = %+v, want first resume", got)
	}
}

func TestDecideTurnRecoveryPersistentAndIdleEscalate(t *testing.T) {
	policy := DefaultTurnRecoveryPolicy()
	stalled := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	login := &ProviderActivityError{Code: "authentication_failed", Message: "Not logged in · Please run /login"}
	if got := decideTurnRecovery(policy, stalled.Add(10*time.Second), stalled, time.Time{}, login, nil); got.action != turnRecoveryWait {
		t.Fatalf("persistent error before confirmation = %+v", got)
	}
	if got := decideTurnRecovery(policy, stalled.Add(policy.PersistentAfter), stalled, time.Time{}, login, nil); got.action != turnRecoveryEscalate || got.reason != "provider_error" {
		t.Fatalf("persistent error = %+v, want escalation without resume", got)
	}
	// Idle without an error waits IdleAfter, and never before the Worker's own lease.
	if got := decideTurnRecovery(policy, stalled.Add(policy.IdleAfter-time.Second), stalled, time.Time{}, nil, nil); got.action != turnRecoveryWait {
		t.Fatalf("idle before threshold = %+v", got)
	}
	lease := stalled.Add(time.Hour)
	if got := decideTurnRecovery(policy, stalled.Add(policy.IdleAfter), stalled, lease, nil, nil); got.action != turnRecoveryWait || !got.due.Equal(lease) {
		t.Fatalf("idle inside declared lease = %+v, want wait until %s", got, lease)
	}
	if got := decideTurnRecovery(policy, lease, stalled, lease, nil, nil); got.action != turnRecoveryEscalate || got.reason != "idle" {
		t.Fatalf("idle past lease = %+v, want idle escalation", got)
	}
}

func TestPaneShowsBackgroundWorkReadsOnlyFooter(t *testing.T) {
	busy := "● Waiting on the render.\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell · esc to interrupt · ← for agents"
	if !paneShowsBackgroundWork(busy) {
		t.Fatal("footer background shell not recognized")
	}
	idle := "● I started 2 shells earlier.\n· 3 shells in the conversation above\n────\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt"
	if paneShowsBackgroundWork(idle) {
		t.Fatal("conversation text above the footer counted as background work")
	}
}

// recoveryLedger is a durable in-memory Work Event store for the supervisor.
type recoveryLedger struct {
	*fakeTurnLedger
	mu        sync.Mutex
	workID    string
	records   []TurnRecovery
	stalls    []TurnStall
	ambiguous []string
	lost      bool
}

func (l *recoveryLedger) TurnRecoveries(sessionID, turnID string) (string, []TurnRecovery, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return "", nil, false, nil
	}
	return l.workID, append([]TurnRecovery(nil), l.records...), true, nil
}

func (l *recoveryLedger) RecordTurnRecovery(record TurnRecovery) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, record)
	return nil
}

func (l *recoveryLedger) EscalateStalledTurn(stall TurnStall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return nil
	}
	l.lost = true
	l.stalls = append(l.stalls, stall)
	return nil
}

func (l *recoveryLedger) MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ambiguous = append(l.ambiguous, proposedTurnID)
	return nil
}

func newRecoveryWatcher(ledger *recoveryLedger, submits *[]string) *Watcher {
	w := &Watcher{turnLedger: ledger}
	w.recovery = &turnRecoverySupervisor{
		policy:    DefaultTurnRecoveryPolicy(),
		inflight:  map[string]bool{},
		nextCheck: map[string]time.Time{},
		submit: func(sessionID, payload, workID, turnID string, at time.Time) (InputResult, error) {
			*submits = append(*submits, payload)
			return InputResult{Outcome: InputAccepted, TurnID: turnID}, nil
		},
		run: func(fn func()) { fn() },
	}
	return w
}

func stalledPollItem(now, settled time.Time, providerID string, failure *ProviderActivityError) probedPollWorker {
	return probedPollWorker{
		preparedPollWorker: preparedPollWorker{
			id: "mewla-worker-a:@1", alive: true, now: now,
			workerSnap: classifier.Worker{ID: "mewla-worker-a:@1", Delegated: true},
			content:    "● Writing the renderer\nAPI Error: Connection lost mid-response.\n────\n❯ \n────\n  ⏵⏵ bypass permissions on",
		},
		hasTurn: true,
		turn: TurnSnapshot{
			SessionID: "mewla-worker-a:@1", TurnID: "turn:original", Status: TurnRunning,
			AcceptedAt: settled.Add(-time.Hour), SignalProtocol: true,
		},
		provider: ProviderActivityObservation{
			ID: providerID, Status: "completed", StartedAt: settled.Add(-time.Minute), SettledAt: settled,
			ProbeState: ProbeStateOK, Error: failure,
		},
	}
}

func TestSuperviseDelegatedTurnsResumesThenEscalatesExactlyOnceAcrossRestart(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits := []string{}
	drop := &ProviderActivityError{Code: "server_error", Message: "API Error: Connection lost mid-response."}
	start := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	w := newRecoveryWatcher(ledger, &submits)

	// Inside the first backoff nothing happens.
	w.superviseDelegatedTurns([]probedPollWorker{stalledPollItem(start.Add(10*time.Second), start, "a1", drop)})
	if len(submits) != 0 {
		t.Fatalf("resumed before backoff: %v", submits)
	}
	settled := start
	for attempt := 1; attempt <= 5; attempt++ {
		if attempt == 3 {
			// A daemon reload: a fresh supervisor reads the durable budget.
			w = newRecoveryWatcher(ledger, &submits)
		}
		due := settled.Add(DefaultTurnRecoveryPolicy().Backoff[attempt-1])
		item := stalledPollItem(due, settled, "a"+string(rune('0'+attempt)), drop)
		w.superviseDelegatedTurns([]probedPollWorker{item})
		// A second poll in the same state does not resubmit.
		item.now = due.Add(time.Second)
		w.superviseDelegatedTurns([]probedPollWorker{item})
		if len(submits) != attempt {
			t.Fatalf("attempt %d: submits=%d, want %d", attempt, len(submits), attempt)
		}
		if !strings.HasPrefix(submits[attempt-1], TurnContinuationPrompt) || !strings.Contains(submits[attempt-1], "--turn-id turn:") {
			t.Fatalf("continuation lacks prompt or turn contract: %q", submits[attempt-1])
		}
		// Recorded at real wall time in production; pin it to the poll clock.
		ledger.records[attempt-1].At = due
		settled = due.Add(time.Minute)
	}
	if got := ledger.records[4].Attempt; got != 5 {
		t.Fatalf("durable attempt numbering = %+v", ledger.records)
	}
	exhausted := stalledPollItem(settled.Add(time.Hour), settled, "a6", drop)
	exhausted.pending = []InputAdmission{{
		ProposedTurnID: "turn:never-signalled", State: InputAdmissionPending, SignalProtocol: true,
		AcceptedAt: settled.Add(-time.Minute),
	}}
	for i := 0; i < 3; i++ {
		exhausted.now = settled.Add(time.Hour + time.Duration(i)*time.Minute)
		w.superviseDelegatedTurns([]probedPollWorker{exhausted})
	}
	if len(submits) != 5 || len(ledger.stalls) != 1 {
		t.Fatalf("exhaustion: submits=%d stalls=%+v, want 5 and exactly one stall", len(submits), ledger.stalls)
	}
	stall := ledger.stalls[0]
	if stall.Reason != "retries_exhausted" || stall.Attempts != 5 || stall.WorkID != "work-1" ||
		!strings.Contains(stall.Summary, "after 5 automatic resumes") || !strings.Contains(stall.Summary, "Connection lost") ||
		!strings.Contains(stall.Summary, "Last output: ● Writing the renderer") {
		t.Fatalf("stall = %+v", stall)
	}
	if len(ledger.ambiguous) != 1 || ledger.ambiguous[0] != "turn:never-signalled" {
		t.Fatalf("stale prompt fence was not released before escalation: %v", ledger.ambiguous)
	}
}

func TestSuperviseDelegatedTurnsLeavesLiveAndBackgroundWorkAlone(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits := []string{}
	w := newRecoveryWatcher(ledger, &submits)
	start := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	later := start.Add(2 * time.Hour)

	running := stalledPollItem(later, start, "a1", nil)
	running.provider.Status = "running"
	background := stalledPollItem(later, start, "a1", nil)
	background.content = "● Waiting on the render\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell · esc to interrupt"
	settled := stalledPollItem(later, start, "a1", nil)
	settled.turn.Status = TurnDone
	approval := stalledPollItem(later, start, "a1", nil)
	approval.activity = classifier.ActivitySignal{State: classifier.StateBlocked}
	fresh := stalledPollItem(later, start, "a1", &ProviderActivityError{Code: "server_error"})
	fresh.pending = []InputAdmission{{ProposedTurnID: "turn:brain-nudge", State: InputAdmissionPending, SignalProtocol: true, AcceptedAt: later.Add(-10 * time.Second)}}
	fresh.provider.SettledAt = later.Add(-5 * time.Second)

	w.superviseDelegatedTurns([]probedPollWorker{running, background, settled, approval, fresh})
	if len(submits) != 0 || len(ledger.stalls) != 0 {
		t.Fatalf("acted on a live/background/settled/approval/fresh-admission Session: submits=%v stalls=%+v", submits, ledger.stalls)
	}
}

func TestStaleSignalAdmissionRequiresProviderIdleAfterPreparation(t *testing.T) {
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	pending := InputAdmission{State: InputAdmissionPending, SignalProtocol: true, AcceptedAt: now.Add(-5 * time.Minute)}
	idle := ProviderActivityObservation{Status: "completed", SettledAt: now.Add(-time.Minute), ProbeState: ProbeStateOK}
	if !staleSignalAdmission(pending, idle, now) {
		t.Fatal("prompt the provider moved past was not stale")
	}
	queued := idle
	queued.Status = "running"
	if staleSignalAdmission(pending, queued, now) {
		t.Fatal("prompt queued behind a running turn released")
	}
	before := idle
	before.SettledAt = now.Add(-10 * time.Minute)
	if staleSignalAdmission(pending, before, now) {
		t.Fatal("prompt prepared after the provider settled released")
	}
	justSettled := idle
	justSettled.SettledAt = now.Add(-10 * time.Second)
	if staleSignalAdmission(pending, justSettled, now) {
		t.Fatal("prompt released before the provider stayed idle")
	}
	review := pending
	review.ClaimToken = "claim"
	if staleSignalAdmission(review, idle, now) {
		t.Fatal("Brain Host delivery admission released")
	}
}

func TestSuperviseDelegatedTurnsResumesPromptThatNeverGainedAnAttempt(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits := []string{}
	w := newRecoveryWatcher(ledger, &submits)
	settled := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	item := stalledPollItem(settled.Add(time.Minute), settled, "a1", &ProviderActivityError{Code: "server_error", Message: "connection lost"})
	item.hasTurn, item.turn = false, TurnSnapshot{}
	item.pending = []InputAdmission{{ProposedTurnID: "turn:initial", State: InputAdmissionPending, SignalProtocol: true, AcceptedAt: settled.Add(-2 * time.Minute)}}
	w.superviseDelegatedTurns([]probedPollWorker{item})
	if len(submits) != 1 || len(ledger.records) != 1 || ledger.records[0].FromTurnID != "turn:initial" {
		t.Fatalf("submits=%d records=%+v", len(submits), ledger.records)
	}
	// Without a stale prompt, a Session with no live Turn is not ours to resume.
	other := item
	other.pending = nil
	other.id = "mewla-worker-b:@2"
	w.superviseDelegatedTurns([]probedPollWorker{other})
	if len(submits) != 1 {
		t.Fatalf("resumed a Session without a delegated prompt: %d", len(submits))
	}
}

func TestPaneTailKeepsTheNewestLines(t *testing.T) {
	pane := strings.Repeat("  turn contract line that fills the excerpt with older text\n", 7) +
		"● API Error: Connection lost mid-response. The response above may be\n  incomplete.\n✻ Baked for 5s · done 1:54\n" +
		"────\n❯ \n────\n  ⏵⏵ bypass permissions on"
	got := paneTail(pane, 12)
	if !strings.HasSuffix(got, "incomplete. ⏎ ✻ Baked for 5s · done 1:54") || !strings.Contains(got, "API Error: Connection lost") || len([]rune(got)) > 600 {
		t.Fatalf("paneTail = %q", got)
	}
}
