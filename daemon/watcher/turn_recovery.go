package watcher

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/google/uuid"
)

// Delegated turn recovery keeps a Worker running through transient provider
// failures without Brain or the user babysitting it.
//
// A delegated signal-protocol Turn is settled only by the Worker's own
// prompt-carried progress report. When the provider's turn ends first — a
// dropped stream, an overloaded upstream, a daemon reload that cut the model
// route — the Session sits idle at its prompt while its Work stays running.
// The supervisor observes exactly that shape from structured evidence (the
// canonical Turn is live, the provider Activity is settled, and the provider's
// own error record says why) and either submits a continuation through the
// normal delegated admission path or escalates once to Brain by releasing the
// Attempt as lost. Every automatic continuation is a durable Work Event, so
// the retry budget survives daemon restarts.

// ProviderErrorClass is the recovery classification of a provider error.
type ProviderErrorClass string

const (
	// ProviderErrorTransient may succeed unchanged: dropped connections,
	// timeouts, overload, 5xx and rate limits.
	ProviderErrorTransient ProviderErrorClass = "transient"
	// ProviderErrorPersistent needs a person or Brain: authentication,
	// billing, unknown model, invalid or oversized requests.
	ProviderErrorPersistent ProviderErrorClass = "persistent"
	// ProviderErrorUnclassified is a failure the provider recorded without a
	// code or status that says what kind it is. It is retried a couple of
	// times in case it was transient, then treated as persistent.
	ProviderErrorUnclassified ProviderErrorClass = "unclassified"
)

var (
	providerErrorPersistentCodes = map[string]bool{
		"authentication_failed": true, "authentication_error": true, "permission_error": true,
		"billing_error": true, "model_not_found": true, "not_found_error": true,
		"invalid_request": true, "invalid_request_error": true, "request_too_large": true,
		"usage_limit_exceeded": true, "quota_exceeded": true,
		// Codex codex_error_info variants.
		"unauthorized": true, "bad_request": true, "context_window_exceeded": true,
		"sandbox_error": true, "session_budget_exceeded": true,
	}
	providerErrorTransientCodes = map[string]bool{
		"server_error": true, "api_error": true, "overloaded": true, "overloaded_error": true,
		"rate_limit": true, "rate_limit_error": true, "timeout": true, "server_overloaded": true,
		"internal_server_error": true, "response_too_many_failed_attempts": true,
		"response_stream_disconnected": true, "response_stream_connection_failed": true,
		"http_connection_failed": true, "flex_unavailable": true,
	}
)

// ClassifyProviderError decides whether resubmitting the same work can succeed,
// from the provider's own structured record only: its error code and the
// upstream HTTP status. An explicit persistent code (credentials, billing,
// request shape) wins, then the status, then a transient code. The message is
// for people and is never read: a failure the provider did not classify gets
// UnclassifiedResumes continuations and then goes to Brain like a persistent one.
//
// Every agent that reports turn errors does so structurally: Claude (StopFailure
// hook, or the API error record's code and status), Codex (app-server
// error classes, or codex_error_info and its status), OpenCode and Pi (typed
// error events). Cursor and Grok record no turn error at all, so there is no
// text for a fallback to read.
func ClassifyProviderError(failure ProviderActivityError) ProviderErrorClass {
	code := strings.ToLower(strings.TrimSpace(failure.Code))
	if providerErrorPersistentCodes[code] {
		return ProviderErrorPersistent
	}
	switch status := failure.HTTPStatus; {
	case status == 408 || status == 409 || status == 425 || status == 429 || status >= 500:
		return ProviderErrorTransient
	case status >= 400:
		return ProviderErrorPersistent
	}
	if providerErrorTransientCodes[code] {
		return ProviderErrorTransient
	}
	return ProviderErrorUnclassified
}

// TurnRecoveryPolicy bounds automatic continuation. Backoff[i] is the wait
// before automatic resume i+1, counted from the stall (or the previous resume,
// whichever is later); len(Backoff) is the per-episode budget. Resumes more
// than ResetAfter apart start a new episode: the Worker made real progress in
// between. A provider stop without any error escalates after IdleAfter, and
// never before the Worker's own declared check-in deadline. A Worker whose
// provider stopped and sits idle at its prompt is told to Brain after
// StoppedAfter instead, whatever lease it declared: nothing is left running
// that could still check in.
type TurnRecoveryPolicy struct {
	// UnclassifiedResumes is how many of Backoff's resumes an unclassified
	// provider error gets before it escalates.
	UnclassifiedResumes int
	Backoff             []time.Duration
	ResetAfter          time.Duration
	IdleAfter           time.Duration
	StoppedAfter        time.Duration
	PersistentAfter     time.Duration
}

// DefaultTurnRecoveryPolicy: 30 s, 2 min, 5 min, 10 min, 10 min; five resumes.
// StoppedAfter covers the transcript flush and a background wake's latency.
func DefaultTurnRecoveryPolicy() TurnRecoveryPolicy {
	return TurnRecoveryPolicy{
		UnclassifiedResumes: 2,
		Backoff:             []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 10 * time.Minute},
		ResetAfter:          30 * time.Minute,
		IdleAfter:           15 * time.Minute,
		StoppedAfter:        90 * time.Second,
		PersistentAfter:     30 * time.Second,
	}
}

// TurnRecovery is one durable automatic continuation of a delegated Work.
type TurnRecovery struct {
	WorkID     string
	SessionID  string
	FromTurnID string
	TurnID     string
	Attempt    int
	Reason     string
	Outcome    InputOutcome
	At         time.Time
}

// TurnStall is the single escalation for a delegated Turn that stopped
// without a terminal report and will not be resumed automatically.
type TurnStall struct {
	WorkID    string
	SessionID string
	TurnID    string
	Reason    string // provider_error | retries_exhausted | idle
	Summary   string
	Attempts  int
	At        time.Time
}

// TurnRecoveryLedger is the durable half of delegated turn recovery.
type TurnRecoveryLedger interface {
	// TurnRecoveries returns the owning Work and its recorded automatic
	// resumes. found is false unless turnID is the live delegated Attempt of
	// sessionID, or its delegated prompt on a Work with no Attempt.
	TurnRecoveries(sessionID, turnID string) (workID string, records []TurnRecovery, found bool, err error)
	RecordTurnRecovery(record TurnRecovery) error
	// EscalateStalledTurn tells Brain once: it releases the exact Attempt as
	// lost, or opens an unknown-delivery review for a prompt that never gained
	// one. A repeated call for the same Turn is a no-op.
	EscalateStalledTurn(stall TurnStall) error
}

type turnRecoveryAction uint8

const (
	turnRecoveryWait turnRecoveryAction = iota
	turnRecoveryResume
	turnRecoveryEscalate
)

type turnRecoveryDecision struct {
	action  turnRecoveryAction
	due     time.Time
	attempt int
	reason  string
}

// turnRecoveryEpisode returns the resumes of the current episode, oldest
// first: the newest consecutive run whose gaps, and whose distance to the
// stall, stay within ResetAfter.
func turnRecoveryEpisode(policy TurnRecoveryPolicy, stalledAt time.Time, records []TurnRecovery) []TurnRecovery {
	episode := []TurnRecovery{}
	next := stalledAt
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if policy.ResetAfter > 0 && next.Sub(record.At) > policy.ResetAfter {
			break
		}
		episode = append([]TurnRecovery{record}, episode...)
		if record.At.Before(next) {
			next = record.At
		}
	}
	return episode
}

// decideTurnRecovery is the pure recovery policy for one stalled Turn.
// stalledAt is when the provider settled; deadline is the Turn's own check-in
// lease; failure is the provider error that settled it, if any.
func decideTurnRecovery(
	policy TurnRecoveryPolicy,
	now, stalledAt, deadline time.Time,
	failure *ProviderActivityError,
	records []TurnRecovery,
) turnRecoveryDecision {
	episode := turnRecoveryEpisode(policy, stalledAt, records)
	base := stalledAt
	if len(episode) > 0 && episode[len(episode)-1].At.After(base) {
		// The last continuation was already submitted for this stall; the
		// next one waits its own backoff from that submission.
		base = episode[len(episode)-1].At
	}
	if failure == nil {
		due := base.Add(policy.IdleAfter)
		if deadline.After(due) {
			due = deadline
		}
		if now.Before(due) {
			return turnRecoveryDecision{action: turnRecoveryWait, due: due}
		}
		return turnRecoveryDecision{action: turnRecoveryEscalate, reason: "idle", attempt: len(episode)}
	}
	class := ClassifyProviderError(*failure)
	budget := len(policy.Backoff)
	if class == ProviderErrorUnclassified {
		budget = min(budget, policy.UnclassifiedResumes)
	}
	if class == ProviderErrorPersistent || class == ProviderErrorUnclassified && budget == 0 {
		due := stalledAt.Add(policy.PersistentAfter)
		if now.Before(due) {
			return turnRecoveryDecision{action: turnRecoveryWait, due: due}
		}
		return turnRecoveryDecision{action: turnRecoveryEscalate, reason: "provider_error", attempt: len(episode)}
	}
	if len(episode) >= budget {
		return turnRecoveryDecision{action: turnRecoveryEscalate, reason: "retries_exhausted", attempt: len(episode)}
	}
	due := base.Add(policy.Backoff[len(episode)])
	if now.Before(due) {
		return turnRecoveryDecision{action: turnRecoveryWait, due: due}
	}
	return turnRecoveryDecision{action: turnRecoveryResume, attempt: len(episode) + 1, reason: ProviderErrorSummary(*failure)}
}

// ProviderErrorSummary is a compact one-line description of a provider error.
func ProviderErrorSummary(failure ProviderActivityError) string {
	parts := []string{}
	if code := strings.TrimSpace(failure.Code); code != "" {
		parts = append(parts, code)
	}
	if failure.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", failure.HTTPStatus))
	}
	head := strings.Join(parts, ", ")
	message := truncateRecoveryText(strings.TrimSpace(failure.Message), 240)
	switch {
	case head == "":
		return message
	case message == "":
		return head
	default:
		return head + ": " + message
	}
}

// TurnContinuationPrompt is the text submitted to resume a cut-off Turn.
const TurnContinuationPrompt = "The previous response was cut off by a provider error. " +
	"Continue the task from where you stopped; don't redo finished steps."

// claudeBackgroundWorkRe matches Claude Code's footer count of background
// shells or agents. A Session idle at its prompt while they run will be woken
// by their completion and is not stalled.
var claudeBackgroundWorkRe = regexp.MustCompile(`·\s*\d+\s+(?:background\s+)?(?:shells?|local agents?|agents?|tasks?|monitors?)\b`)

func paneShowsBackgroundWork(pane string) bool {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	seen := 0
	for index := len(lines) - 1; index >= 0 && seen < 3; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		seen++
		if claudeBackgroundWorkRe.MatchString(line) {
			return true
		}
	}
	return false
}

// paneTail returns the last non-empty pane lines above the provider's own
// prompt chrome, compacted for an escalation summary.
func paneTail(pane string, lines int) string {
	all := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	out := []string{}
	for index := len(all) - 1; index >= 0 && len(out) < lines; index-- {
		line := strings.TrimSpace(all[index])
		if line == "" || strings.Trim(line, "─━-—=") == "" || strings.HasPrefix(line, "❯") ||
			strings.HasPrefix(line, "⏵") || strings.HasPrefix(line, ">") {
			continue
		}
		out = append([]string{line}, out...)
	}
	// The newest lines say why the Worker stopped; keep the end.
	runes := []rune(strings.Join(out, " ⏎ "))
	if len(runes) > 600 {
		return "…" + string(runes[len(runes)-599:])
	}
	return string(runes)
}

func truncateRecoveryText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

// turnRecoverySupervisor holds only in-flight submission state. Budgets and
// history are durable Work Events read through TurnRecoveryLedger.
type turnRecoverySupervisor struct {
	mu       sync.Mutex
	policy   TurnRecoveryPolicy
	inflight map[string]bool
	// nextCheck throttles durable reads for a Session whose stall decision
	// is not yet due; it is a cache of the decision, never budget state.
	nextCheck map[string]time.Time
	// idleSince is when the supervisor first saw a Session in its current
	// stalled shape. A Worker idle at its prompt while a background Monitor or
	// shell runs is not stalled; the moment that work ends, and before the
	// provider's next Activity shows, the Session looks stopped for a few
	// seconds. Waiting is counted from the first poll that saw it stalled,
	// never from when the provider settled.
	idleSince map[string]time.Time
	submit    func(sessionID, payload, workID, turnID string, at time.Time) (InputResult, error)
	run       func(func())
}

func newTurnRecoverySupervisor(w *Watcher) *turnRecoverySupervisor {
	return &turnRecoverySupervisor{
		policy:    DefaultTurnRecoveryPolicy(),
		inflight:  map[string]bool{},
		nextCheck: map[string]time.Time{},
		idleSince: map[string]time.Time{},
		submit: func(sessionID, payload, workID, turnID string, at time.Time) (InputResult, error) {
			return w.SubmitDelegatedWorkInput(sessionID, payload, workID, turnID, "", "", at)
		},
		run: func(fn func()) { go fn() },
	}
}

// SetTurnRecoveryPolicy replaces the recovery policy (tests and drills).
func (w *Watcher) SetTurnRecoveryPolicy(policy TurnRecoveryPolicy) {
	supervisor := w.turnRecovery()
	supervisor.mu.Lock()
	supervisor.policy = policy
	supervisor.mu.Unlock()
}

func (w *Watcher) turnRecovery() *turnRecoverySupervisor {
	w.recoveryOnce.Do(func() {
		if w.recovery == nil {
			w.recovery = newTurnRecoverySupervisor(w)
		}
	})
	return w.recovery
}

// superviseDelegatedTurns runs after a complete poll. It never blocks the
// poll: submissions run on their own goroutine, one per Session at a time.
func (w *Watcher) superviseDelegatedTurns(results []probedPollWorker) {
	ledger, ok := w.turnLedger.(TurnRecoveryLedger)
	if !ok {
		return
	}
	supervisor := w.turnRecovery()
	for _, item := range results {
		stall, stalled := stalledDelegatedTurn(item)
		supervisor.mu.Lock()
		key := item.id + "\x00" + stall.turnID + "\x00" + item.provider.ID
		if !stalled {
			for existing := range supervisor.nextCheck {
				if strings.HasPrefix(existing, item.id+"\x00") {
					delete(supervisor.nextCheck, existing)
				}
			}
			for existing := range supervisor.idleSince {
				if strings.HasPrefix(existing, item.id+"\x00") {
					delete(supervisor.idleSince, existing)
				}
			}
		} else {
			if supervisor.idleSince == nil {
				supervisor.idleSince = map[string]time.Time{}
			}
			if _, seen := supervisor.idleSince[key]; !seen {
				supervisor.idleSince[key] = item.now
			}
		}
		idleSince := supervisor.idleSince[key]
		due, throttled := supervisor.nextCheck[key]
		busy := supervisor.inflight[item.id]
		policy := supervisor.policy
		supervisor.mu.Unlock()
		if !stalled || busy || (throttled && item.now.Before(due)) {
			continue
		}
		// A prompt the provider took its turn over without admitting (a
		// keystroke sent into a prompt never becomes a user prompt) holds the
		// Work's active-Attempt fence: every later send or resume fails with
		// "Work already has an active Attempt". It is released as
		// delivery-unknown as soon as it is proven stale, whether or not a
		// recovery record exists for it.
		w.releaseStaleAdmissions(item.id, stall.stale)
		if stall.pendingFresh {
			// An in-flight admission still owns the next prompt.
			supervisor.recheck(item.id, key, item.now.Add(turnRecoveryRecheck))
			continue
		}
		workID, records, found, err := ledger.TurnRecoveries(item.id, stall.turnID)
		if err != nil || !found {
			supervisor.recheck(item.id, key, item.now.Add(turnRecoveryRecheck))
			continue
		}
		deadline := stall.deadline
		if stall.stopped && policy.StoppedAfter > 0 {
			policy.IdleAfter, deadline = policy.StoppedAfter, time.Time{}
		}
		settledAt := stall.settledAt
		if item.provider.Error == nil && idleSince.After(settledAt) {
			settledAt = idleSince
		}
		decision := decideTurnRecovery(policy, item.now, settledAt, deadline, item.provider.Error, records)
		if stall.stopped && decision.reason == "idle" {
			decision.reason = "stopped"
		}
		switch decision.action {
		case turnRecoveryWait:
			next := item.now.Add(turnRecoveryRecheck)
			if decision.due.Before(next) {
				next = decision.due
			}
			supervisor.recheck(item.id, key, next)
		case turnRecoveryResume:
			supervisor.mu.Lock()
			if supervisor.inflight[item.id] {
				supervisor.mu.Unlock()
				continue
			}
			supervisor.inflight[item.id] = true
			supervisor.mu.Unlock()
			// A failed delivery retries on a later check, never every poll.
			supervisor.recheck(item.id, key, item.now.Add(turnRecoveryRecheck))
			sessionID, fromTurn := item.id, stall.turnID
			supervisor.run(func() {
				defer func() {
					supervisor.mu.Lock()
					delete(supervisor.inflight, sessionID)
					supervisor.mu.Unlock()
				}()
				w.resumeStalledTurn(ledger, supervisor, sessionID, workID, fromTurn, decision)
			})
		case turnRecoveryEscalate:
			supervisor.recheck(item.id, key, item.now.Add(turnRecoveryRecheck))
			summary := stallSummary(decision, item.provider.Error, item.content)
			if err := ledger.EscalateStalledTurn(TurnStall{
				WorkID: workID, SessionID: item.id, TurnID: stall.turnID,
				Reason: decision.reason, Summary: summary, Attempts: decision.attempt, At: item.now,
			}); err != nil {
				log.Printf("delegated turn recovery: escalate %s turn %s: %v", item.id, stall.turnID, err)
			}
		}
	}
}

type delegatedStall struct {
	turnID       string
	deadline     time.Time
	settledAt    time.Time
	pendingFresh bool
	stale        []string
	// stopped marks a live turn whose provider ended without an error and
	// whose pane is idle at its prompt: the Worker has nothing left running.
	stopped bool
}

// turnRecoveryRecheck bounds how long a Session's stall decision is cached.
const turnRecoveryRecheck = 15 * time.Second

func (s *turnRecoverySupervisor) recheck(sessionID, key string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for existing := range s.nextCheck {
		if strings.HasPrefix(existing, sessionID+"\x00") && existing != key {
			delete(s.nextCheck, existing)
		}
	}
	s.nextCheck[key] = at
}

// stalledDelegatedTurn recognizes a delegated Session idle at its prompt
// after its provider settled, while its Work still waits for a terminal
// report. The stalled identity is the live signal Turn, or — when the drop
// came before the Worker's first progress report — the delegated prompt that
// never gained an Attempt.
func stalledDelegatedTurn(item probedPollWorker) (delegatedStall, bool) {
	turn, provider := item.turn, item.provider
	if item.turnErr != nil || !item.alive || !item.workerSnap.Delegated || item.workerSnap.Hidden ||
		provider.ProbeState != ProbeStateOK || !providerActivityTerminal(provider.Status) || provider.SettledAt.IsZero() ||
		item.activity.State == classifier.StateBlocked {
		return delegatedStall{}, false
	}
	if provider.Error == nil && paneShowsBackgroundWork(item.content) {
		return delegatedStall{}, false
	}
	stall := delegatedStall{settledAt: provider.SettledAt}
	var newestStale *InputAdmission
	for index, pending := range item.pending {
		if staleSignalAdmission(pending, provider, item.now) {
			stall.stale = append(stall.stale, pending.ProposedTurnID)
			if newestStale == nil || pending.AcceptedAt.After(newestStale.AcceptedAt) {
				newestStale = &item.pending[index]
			}
		} else {
			stall.pendingFresh = true
		}
	}
	switch {
	case item.hasTurn && turn.SignalProtocol && !TurnTerminal(turn.Status) && turn.ControlState != TurnControlOwnershipLost:
		if provider.SettledAt.Before(turn.AcceptedAt) {
			return delegatedStall{}, false
		}
		stall.turnID, stall.deadline = turn.TurnID, turn.LeaseDeadline
		// A Blocked turn already holds an actionable request for Brain.
		stall.stopped = provider.Error == nil && turn.Status != TurnBlocked &&
			isWorkerInputReady(item.workerSnap.Command, item.content)
	case (!item.hasTurn || TurnTerminal(turn.Status)) && newestStale != nil:
		stall.turnID = newestStale.ProposedTurnID
	default:
		return delegatedStall{}, false
	}
	return stall, true
}

// signalAdmissionStaleAfter is how long a provider must stay idle after
// settling, past a delegated prompt's preparation, before that prompt's
// missing progress signal proves nothing more will arrive.
const signalAdmissionStaleAfter = 45 * time.Second

// staleSignalAdmission reports a prepared delegated prompt that the provider
// already took its turn over: its Activity settled after the preparation and
// has stayed idle since. Its delivery is unknown, not failed.
func staleSignalAdmission(pending InputAdmission, provider ProviderActivityObservation, now time.Time) bool {
	return pending.State == InputAdmissionPending && pending.SignalProtocol && pending.ClaimToken == "" &&
		pending.Purpose == "" && provider.ProbeState == ProbeStateOK && providerActivityTerminal(provider.Status) &&
		!provider.SettledAt.IsZero() && provider.SettledAt.After(pending.AcceptedAt) &&
		now.Sub(provider.SettledAt) >= signalAdmissionStaleAfter
}

// releaseStaleAdmissions closes prepared prompt admissions the provider took
// its turn over without ever admitting, as delivery-unknown. They are not
// replayed; the fence they hold on the Work is what is released.
func (w *Watcher) releaseStaleAdmissions(sessionID string, stale []string) {
	ambiguity, ok := w.turnLedger.(InputAdmissionAmbiguityLedger)
	if !ok {
		return
	}
	for _, pending := range stale {
		if err := ambiguity.MarkInputAdmissionAmbiguous(sessionID, pending,
			"provider settled and stayed idle without this prompt's progress signal"); err != nil {
			log.Printf("delegated turn recovery: release stale admission %s: %v", pending, err)
		}
	}
}

func (w *Watcher) resumeStalledTurn(
	ledger TurnRecoveryLedger,
	supervisor *turnRecoverySupervisor,
	sessionID, workID, fromTurn string,
	decision turnRecoveryDecision,
) {
	// Re-read under the durable ledger: a concurrent poll or Brain may have
	// already continued this Turn.
	_, records, found, err := ledger.TurnRecoveries(sessionID, fromTurn)
	if err != nil || !found {
		return
	}
	for _, record := range records {
		if record.FromTurnID == fromTurn && record.Attempt >= decision.attempt {
			return
		}
	}
	turnID := "turn:" + uuid.NewString()
	at := time.Now().UTC()
	result, submitErr := supervisor.submit(sessionID, DelegatedLifecyclePayload(TurnContinuationPrompt, turnID), workID, turnID, at)
	outcome := result.Outcome
	if submitErr != nil {
		outcome = InputOutcomeFromError(submitErr)
	}
	if outcome == InputNotSubmitted {
		// Definitely not delivered: no budget consumed; the next poll retries.
		log.Printf("delegated turn recovery: resume %s (work %s) not submitted: %v", sessionID, workID, submitErr)
		return
	}
	if err := ledger.RecordTurnRecovery(TurnRecovery{
		WorkID: workID, SessionID: sessionID, FromTurnID: fromTurn, TurnID: turnID,
		Attempt: decision.attempt, Reason: decision.reason, Outcome: outcome, At: at,
	}); err != nil {
		log.Printf("delegated turn recovery: record resume %s (work %s): %v", sessionID, workID, err)
	}
}

func stallSummary(decision turnRecoveryDecision, failure *ProviderActivityError, pane string) string {
	var head string
	switch decision.reason {
	case "retries_exhausted":
		head = fmt.Sprintf("Worker stalled: provider error persisted after %d automatic resumes", decision.attempt)
	case "provider_error":
		head = "Worker stalled: provider error needs attention; not resumed automatically"
	case "stopped":
		head = "Worker stopped; review the result"
	default:
		head = "Worker stopped without a terminal progress report"
	}
	if failure != nil {
		head += " (" + ProviderErrorSummary(*failure) + ")"
	}
	if tail := paneTail(pane, 12); tail != "" {
		head += ". Last output: " + tail
	}
	return head
}

// DelegatedLifecyclePayload exposes a pending submission's sole random
// identity to the Worker in the prompt that identity owns. The literal flag is
// intentionally turn-scoped rather than process environment: a reusable
// Session receives a different identity with every admitted prompt.
func DelegatedLifecyclePayload(payload, turnID string) string {
	contract := fmt.Sprintf(`Mewla delegated turn contract:
Use this turn's identity for every progress command; never reuse an earlier turn's identity:
  "$MEWLA_WORKER_PROGRESS_CMD" worker progress --turn-id %s --status running --phase working --attention none --summary "Current work" --lease 300`, strings.TrimSpace(turnID))
	return payload + "\n\n" + contract
}
