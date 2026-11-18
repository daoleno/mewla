package watcher

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A delegated Claude Worker whose conversation grew too large to send stops
// on "Request too large … Run /compact": resubmitting the same work fails
// the same way, so it is a persistent error and used to go straight to
// Brain, who could not send a slash command to a Session it no longer owned.
// The agent's own remedy loses nothing the Worker needs, so Mewla applies it:
// it types /compact into the Session (SendProviderCommand), waits for Claude
// to record the compaction (compact_boundary, ProviderActivityObservation.
// CompactedAt; both this and the 413 are read from the transcript, which
// alone records them), and submits the continuation through the normal delegated
// admission path. Each step is a durable Work Event, so Brain sees what
// happened. A compaction that does not show up, or a Turn that is too large
// again after one, goes to Brain once, saying so.
//
// Only Claude: its transcript records the compaction. Codex compacts on its
// own at its context limit, and pi on a context overflow; Brain can still
// send either one /compact (worker send -command).

// compactReasonPrefix marks the recovery records of a compaction.
const compactReasonPrefix = "compact"

// compactWait bounds the wait for the compaction to be recorded.
const compactWait = 10 * time.Minute

// compactPoll is how often the provider is looked at meanwhile.
const compactPoll = 2 * time.Second

// CompactedContinuationPrompt resumes a Turn after Mewla compacted the
// conversation.
const CompactedContinuationPrompt = "Your conversation grew too large to send, so Mewla compacted it (/compact). " +
	"Continue the task from where you stopped; don't redo finished steps."

// providerErrorTooLarge: the provider refused the request for its size (HTTP
// 413, as Claude's transcript records it). Not an error code: the agents' own
// classes put this together with others ("invalid_request", and
// "max_output_tokens" counts as a context error), and compacting helps only
// this one.
func providerErrorTooLarge(failure ProviderActivityError) bool {
	return failure.HTTPStatus == 413
}

// autoCompactCommand is the compaction command Mewla runs itself for the
// agent of command, "" when it does not (see above).
func autoCompactCommand(command string) string {
	if isClaudeCommand(command) {
		return CompactCommand
	}
	return ""
}

// decideCompaction handles a stall on a too-large request for an agent Mewla
// compacts. handled is false for every other stall, which decideTurnRecovery
// decides as before.
func decideCompaction(
	policy TurnRecoveryPolicy,
	stalledAt time.Time,
	failure *ProviderActivityError,
	compactedAt time.Time,
	command string,
	records []TurnRecovery,
) (turnRecoveryDecision, bool) {
	if failure == nil || autoCompactCommand(command) == "" || !providerErrorTooLarge(*failure) {
		return turnRecoveryDecision{}, false
	}
	episode := turnRecoveryEpisode(policy, stalledAt, records)
	var compaction *TurnRecovery
	resumed := false
	for index := range episode {
		record := episode[index]
		if !strings.HasPrefix(record.Reason, compactReasonPrefix) {
			continue
		}
		if strings.HasPrefix(record.TurnID, compactTurnPrefix) {
			compaction, resumed = &episode[index], false
		} else {
			resumed = true
		}
	}
	switch {
	case compaction == nil:
		return turnRecoveryDecision{action: turnRecoveryCompact, attempt: len(episode) + 1, reason: ProviderErrorSummary(*failure)}, true
	case !resumed && !compactedAt.IsZero() && compactedAt.After(compaction.At):
		// Compacted, but the continuation never went out (a restart in
		// between): send it now.
		return turnRecoveryDecision{action: turnRecoveryResumeCompacted, attempt: len(episode) + 1}, true
	default:
		// Compacted and resumed, and too large again; or the compaction never
		// happened.
		return turnRecoveryDecision{action: turnRecoveryEscalate, reason: "compaction_failed", attempt: len(episode)}, true
	}
}

// compactTurnPrefix names the recovery record of the /compact itself (it
// admits no Turn, so its identity is its own).
const compactTurnPrefix = "compact:"

// compactStalledTurn sends the agent's /compact, waits for the compaction,
// then resumes the Turn; or tells Brain it could not.
func (w *Watcher) compactStalledTurn(
	ledger TurnRecoveryLedger,
	supervisor *turnRecoverySupervisor,
	sessionID, workID, fromTurn, command string,
	decision turnRecoveryDecision,
) {
	_, records, found, err := ledger.TurnRecoveries(sessionID, fromTurn)
	if err != nil || !found {
		return
	}
	for _, record := range records {
		if record.FromTurnID == fromTurn && strings.HasPrefix(record.TurnID, compactTurnPrefix) {
			return
		}
	}
	if supervisor.command == nil || supervisor.observe == nil {
		return
	}
	sleep := supervisor.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sent := time.Now().UTC()
	compact := autoCompactCommand(command)
	if err := supervisor.command(sessionID, compact); err != nil {
		// Not sent: nothing is recorded and a later check tries again.
		log.Printf("delegated turn recovery: %s for %s (work %s) not sent: %v", compact, sessionID, workID, err)
		return
	}
	if err := ledger.RecordTurnRecovery(TurnRecovery{
		WorkID: workID, SessionID: sessionID, FromTurnID: fromTurn, TurnID: compactTurnPrefix + uuid.NewString(),
		Attempt: decision.attempt, Reason: compactReasonPrefix + ": the conversation is too large to send (" + decision.reason + "); Mewla sent " + compact,
		Outcome: InputAccepted, At: sent,
	}); err != nil {
		log.Printf("delegated turn recovery: record %s for %s (work %s): %v", compact, sessionID, workID, err)
	}
	why := fmt.Sprintf("no compaction was recorded within %s", compactWait)
	compacted := false
	for deadline := sent.Add(compactWait); time.Now().Before(deadline); sleep(compactPoll) {
		observation, ok := supervisor.observe(sessionID)
		if !ok {
			continue
		}
		if observation.CompactedAt.After(sent) {
			compacted = true
			break
		}
		if observation.CompactFailedAt.After(sent) {
			why = "the agent said: " + firstNonEmptyString(observation.CompactFailure, "compaction failed")
			break
		}
		if observation.Error != nil && observation.SettledAt.After(sent) {
			why = "the compaction itself failed (" + ProviderErrorSummary(*observation.Error) + ")"
			break
		}
	}
	if !compacted {
		summary := "Worker stalled: its conversation is too large to send, and Mewla's " + compact + " did not compact it: " + why
		if err := ledger.EscalateStalledTurn(TurnStall{
			WorkID: workID, SessionID: sessionID, TurnID: fromTurn,
			Reason: "compaction_failed", Summary: summary, Attempts: decision.attempt, At: time.Now().UTC(),
		}); err != nil {
			log.Printf("delegated turn recovery: escalate %s turn %s: %v", sessionID, fromTurn, err)
		}
		return
	}
	w.submitContinuation(ledger, supervisor, sessionID, workID, fromTurn, decision.attempt+1,
		compactReasonPrefix+"ed: resumed after Mewla compacted the conversation", CompactedContinuationPrompt)
}
