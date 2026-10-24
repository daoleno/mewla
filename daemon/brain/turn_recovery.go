package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/google/uuid"
)

// Work Event kinds written by delegated turn recovery. An automatic resume is
// audit history; a stall is the evidence row the canonical turn_lost review
// carries to Brain (its dedupe key is Turn-scoped like other session.* rows).
const (
	workEventKindAutoResumed = "session.auto_resumed"
	workEventKindStalled     = "session.stale"
)

type turnRecoveryDetails struct {
	SessionID  string `json:"session_id"`
	FromTurnID string `json:"from_turn_id"`
	TurnID     string `json:"turn_id"`
	Attempt    int    `json:"attempt"`
	Reason     string `json:"reason,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
}

type turnStallDetails struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	Reason    string `json:"reason"`
	Attempts  int    `json:"automatic_resumes"`
}

var _ watcher.TurnRecoveryLedger = (*Store)(nil)

// liveDelegatedAttempt returns the Work whose live delegated Attempt is
// exactly (sessionID, turnID).
func (s *Store) liveDelegatedAttempt(sessionID, turnID string) (*lifecycle.State, bool) {
	st, found := s.fsmWorkByAttemptSession(sessionID)
	if !found || st.Attempt == nil || !st.Attempt.Delegated || string(st.Attempt.TurnToken) != strings.TrimSpace(turnID) ||
		st.Status.Terminal() {
		return nil, false
	}
	return st, true
}

// unadmittedDelegatedPrompt returns the Work whose delegated prompt turnID on
// sessionID never gained an Attempt: the Work has none and is still open.
func (s *Store) unadmittedDelegatedPrompt(sessionID, turnID string) (*lifecycle.State, bool) {
	token := lifecycle.TurnToken(strings.TrimSpace(turnID))
	return s.fsm.Find(func(st *lifecycle.State) bool {
		admission := st.AdmissionByToken(token)
		return admission != nil && st.Attempt == nil && !st.Status.Terminal() && admission.SessionID == sessionID &&
			admission.SignalProtocol && admission.ClaimToken == "" && admission.Purpose == "" &&
			(admission.Status == lifecycle.AdmissionPrepared || admission.Status == lifecycle.AdmissionAmbiguous)
	})
}

// TurnRecoveries implements watcher.TurnRecoveryLedger. The records are the
// durable session.auto_resumed Work Events, so the retry budget survives a
// daemon restart.
func (s *Store) TurnRecoveries(sessionID, turnID string) (string, []watcher.TurnRecovery, bool, error) {
	if s == nil || s.fsm == nil {
		return "", nil, false, fmt.Errorf("brain store is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	st, found := s.liveDelegatedAttempt(sessionID, turnID)
	if !found {
		st, found = s.unadmittedDelegatedPrompt(sessionID, turnID)
	}
	if !found {
		return "", nil, false, nil
	}
	workID := string(st.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return "", nil, false, err
	}
	records := []watcher.TurnRecovery{}
	for _, event := range database.BrainWorkEvents {
		if event.WorkID != workID || event.Kind != workEventKindAutoResumed {
			continue
		}
		var details turnRecoveryDetails
		if json.Unmarshal([]byte(event.DetailsJSON), &details) != nil || details.SessionID != sessionID {
			continue
		}
		records = append(records, watcher.TurnRecovery{
			WorkID: workID, SessionID: details.SessionID, FromTurnID: details.FromTurnID, TurnID: details.TurnID,
			Attempt: details.Attempt, Reason: details.Reason, Outcome: watcher.InputOutcome(details.Outcome),
			At: event.CreatedAt,
		})
	}
	sort.SliceStable(records, func(left, right int) bool { return records[left].At.Before(records[right].At) })
	return workID, records, true, nil
}

// RecordTurnRecovery appends one automatic resume as a non-actionable Work
// Event. The continuation's own Turn identity dedupes it.
func (s *Store) RecordTurnRecovery(record watcher.TurnRecovery) error {
	if s == nil {
		return fmt.Errorf("brain store is not configured")
	}
	if strings.TrimSpace(record.WorkID) == "" || strings.TrimSpace(record.TurnID) == "" || record.At.IsZero() {
		return fmt.Errorf("turn recovery requires work, turn and time")
	}
	details, err := json.Marshal(turnRecoveryDetails{
		SessionID: record.SessionID, FromTurnID: record.FromTurnID, TurnID: record.TurnID,
		Attempt: record.Attempt, Reason: record.Reason, Outcome: string(record.Outcome),
	})
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("Automatic resume %d after a provider error", record.Attempt)
	if reason := strings.TrimSpace(record.Reason); reason != "" {
		summary += ": " + reason
	}
	_, err = s.appendWorkAuditEvent(WorkEvent{
		WorkID: record.WorkID, Kind: workEventKindAutoResumed,
		DedupeKey:  "session:" + record.SessionID + ":turn:" + record.FromTurnID + ":auto_resumed:" + record.TurnID,
		PayloadRef: "session:" + record.SessionID, SourceName: record.SessionID,
		Summary: summary, DetailsJSON: string(details), CreatedAt: record.At.UTC(),
	})
	return err
}

// EscalateStalledTurn tells Brain, once, that a delegated Turn stopped and
// will not be resumed automatically: it records the stall evidence and
// releases the exact Attempt as lost, which opens the turn_lost review. A
// repeated or stale call is a no-op.
func (s *Store) EscalateStalledTurn(stall watcher.TurnStall) error {
	if s == nil || s.fsm == nil {
		return fmt.Errorf("brain store is not configured")
	}
	st, found := s.liveDelegatedAttempt(stall.SessionID, stall.TurnID)
	if !found {
		if prompt, unadmitted := s.unadmittedDelegatedPrompt(stall.SessionID, stall.TurnID); unadmitted &&
			string(prompt.ID) == strings.TrimSpace(stall.WorkID) {
			return s.escalateUnadmittedPrompt(prompt, stall)
		}
		return nil
	}
	if string(st.ID) != strings.TrimSpace(stall.WorkID) {
		return nil
	}
	details, err := json.Marshal(turnStallDetails{
		SessionID: stall.SessionID, TurnID: stall.TurnID, Reason: stall.Reason, Attempts: stall.Attempts,
	})
	if err != nil {
		return err
	}
	// The evidence row comes first so the review projected by the loss
	// carries its summary to Brain.
	if _, err := s.appendWorkAuditEvent(WorkEvent{
		WorkID: stall.WorkID, Kind: workEventKindStalled,
		DedupeKey: "session:" + stall.SessionID + ":turn:" + stall.TurnID + ":" + workEventKindStalled,
		// The loss review is identified by its Turn token; the evidence keeps it.
		PayloadRef: stall.TurnID, SourceName: stall.SessionID,
		Summary: stall.Summary, DetailsJSON: string(details), CreatedAt: stall.At.UTC(),
	}); err != nil {
		return err
	}
	identity := lifecycle.AttemptIdentity{SessionID: st.Attempt.SessionID, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation}
	if _, err := s.fsm.ReportTurnLost(st.ID, identity, "worker_stalled: "+stall.Reason); err != nil {
		if _, live := s.liveDelegatedAttempt(stall.SessionID, stall.TurnID); !live {
			return nil
		}
		return err
	}
	return s.SyncWorkProjection(string(st.ID))
}

// escalateUnadmittedPrompt opens the unknown-delivery review for a delegated
// prompt whose provider turn ended before the Worker ever reported progress.
// The prompt may have been read; it is never replayed.
func (s *Store) escalateUnadmittedPrompt(st *lifecycle.State, stall watcher.TurnStall) error {
	details, err := json.Marshal(turnStallDetails{
		SessionID: stall.SessionID, TurnID: stall.TurnID, Reason: stall.Reason, Attempts: stall.Attempts,
	})
	if err != nil {
		return err
	}
	if _, err := s.appendWorkAuditEvent(WorkEvent{
		WorkID: stall.WorkID, Kind: "session.uncertain",
		DedupeKey:  "session:" + stall.SessionID + ":turn:" + stall.TurnID + ":" + workEventKindStalled,
		PayloadRef: stall.TurnID, SourceName: stall.SessionID,
		Summary: stall.Summary, DetailsJSON: string(details), CreatedAt: stall.At.UTC(),
	}); err != nil {
		return err
	}
	if _, err := s.fsm.OpenReview(st.ID, "submission_ambiguous", stall.TurnID); err != nil {
		return err
	}
	return s.SyncWorkProjection(string(st.ID))
}

// appendWorkAuditEvent appends one deduped non-actionable Work Event without
// changing Work revision. It returns false for an existing dedupe key.
func (s *Store) appendWorkAuditEvent(event WorkEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return false, err
	}
	for _, current := range database.BrainWorkEvents {
		if current.WorkID == event.WorkID && current.DedupeKey == event.DedupeKey {
			return false, nil
		}
	}
	itemIndex := workIndex(database.BrainWork, event.WorkID)
	event.ID = uuid.NewString()
	event.Actionable = false
	if _, err := appendWorkEventLocked(&database, itemIndex, event, false); err != nil {
		return false, err
	}
	if err := s.persistPresentationLocked(database); err != nil {
		return false, err
	}
	s.broadcastWorkChange(event.WorkID)
	return true, nil
}

var _ watcher.TurnRecoveryLedger = (*Service)(nil)

func (s *Service) TurnRecoveries(sessionID, turnID string) (string, []watcher.TurnRecovery, bool, error) {
	if s == nil || s.store == nil {
		return "", nil, false, nil
	}
	return s.store.TurnRecoveries(sessionID, turnID)
}

func (s *Service) RecordTurnRecovery(record watcher.TurnRecovery) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("brain service is not configured")
	}
	return s.store.RecordTurnRecovery(record)
}

func (s *Service) EscalateStalledTurn(stall watcher.TurnStall) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("brain service is not configured")
	}
	return s.store.EscalateStalledTurn(stall)
}

// ResolveAnsweredWorkerBlock records Brain's answer to a delegated Worker's
// blocked action as the disposition of the review that raised it: Brain handed
// the decision back, so the Work waits on the exact Session Turn. Without it
// the Host turn ends with the review still open and the lifecycle wakes Brain
// again to reconcile a turn that did all it had to do. It reports whether a
// delivered needs_input review of that Session was resolved; any other Work
// state is left alone.
func (s *Store) ResolveAnsweredWorkerBlock(sessionID string) (bool, error) {
	if s == nil || s.fsm == nil {
		return false, fmt.Errorf("brain store is not configured")
	}
	st, found := s.fsmWorkByAttemptSession(sessionID)
	if !found || st.Attempt == nil || st.Review == nil || st.Review.Reason != "session.needs_input" {
		return false, nil
	}
	item, err := s.Work(string(st.ID))
	if err != nil {
		return false, err
	}
	if !reviewDeliveredAwaitingDisposition(item.Review) || item.Review.EventID != st.Review.EventID {
		return false, nil
	}
	lease := item.Review.Lease
	_, _, err = s.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionWait,
		Wake: &WorkWake{Kind: WorkWakeSessionTerminal,
			Ref: SessionTerminalWakeRef(st.Attempt.SessionID, string(st.Attempt.TurnToken))},
		Summary: "Brain answered the Worker's blocked action; the Work waits for the Worker.",
	})
	if errors.Is(err, ErrEventHandled) || errors.Is(err, ErrEventClaim) {
		return false, nil
	}
	return err == nil, err
}
